package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"frpmon/internal/config"
	"frpmon/internal/store"
)

// Service AI 诊断：只做分析报告，绝不生成可执行命令、绝不执行任何操作。
type Service struct {
	db  *store.DB
	cfg *config.Manager
}

type Result struct {
	TS       time.Time `json:"ts"`
	Machine  string    `json:"machine"`
	Report   string    `json:"reportOverall"`
	Score    int       `json:"reportScore"`
	Text     string    `json:"text"`
	Flagged  bool      `json:"flagged"` // 输出疑似含命令内容（仅标注，无执行路径）
	Err      string    `json:"err,omitempty"`
}

func New(db *store.DB, cfg *config.Manager) *Service {
	return &Service{db: db, cfg: cfg}
}

// Diagnose 对一台机器的体检报告执行 AI 诊断。
// 输入：最近体检报告 + 机器快照摘要；输出：纯文本分析。
func (s *Service) Diagnose(machineID int64) (*Result, error) {
	c := s.cfg.Get()
	if !c.AI.Enabled {
		return nil, fmt.Errorf("AI 诊断未启用，请在设置中配置 Provider 并开启")
	}
	if c.AI.ProviderURL == "" || c.AI.Model == "" {
		return nil, fmt.Errorf("AI 诊断未配置完整（需要 Provider 地址与模型）")
	}
	encKey, err := s.db.GetSetting("ai_api_key")
	if err != nil {
		return nil, fmt.Errorf("读取 API Key 失败: %w", err)
	}
	if encKey == "" {
		return nil, fmt.Errorf("未配置 API Key")
	}
	apiKey, err := s.db.DecryptSecret(encKey)
	if err != nil {
		return nil, fmt.Errorf("解密 API Key 失败: %w", err)
	}

	m, err := s.db.GetMachineByID(machineID)
	if err != nil {
		return nil, err
	}
	// 最近体检报告
	reports, err := s.db.ListHealthReports(machineID, 1)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("该机器没有体检报告，请先执行一键体检")
	}
	rep := reports[0]
	snap, err := s.db.GetSnapshot(machineID)
	if err != nil {
		return nil, fmt.Errorf("无采集快照: %w", err)
	}

	prompt := buildPrompt(m.Name, rep.Overall, rep.Score, rep.ItemsJSON, snap.Data)
	text, err := callLLM(c.AI.ProviderURL, c.AI.Model, apiKey, prompt, c.AI.TimeoutSec)
	res := &Result{
		TS: time.Now(), Machine: m.Name,
		Report: rep.Overall, Score: rep.Score,
	}
	if err != nil {
		res.Err = err.Error()
		_ = s.db.Log(0, "system", "ai_diagnose", m.Name, "失败: "+err.Error())
		return res, fmt.Errorf("调用 LLM 失败: %w", err)
	}
	res.Text = text
	res.Flagged = containsCommandLike(text)
	_ = s.db.Log(0, "system", "ai_diagnose", m.Name,
		fmt.Sprintf("评分 %d (%s)%s", rep.Score, rep.Overall, map[bool]string{true: "，含疑似命令已标注", false: ""}[res.Flagged]))
	return res, nil
}

// buildPrompt 组装诊断提示词：明确禁止输出可执行命令。
func buildPrompt(machine, overall string, score int, itemsJSON, snapJSON string) string {
	return fmt.Sprintf(`你是服务器运维诊断助手。以下是机器 %s 的一键体检报告（评分 %d/100，结论 %s）：

【体检报告】
%s

【补充监控数据（截断）】
%s

请用中文输出诊断分析，要求：
1. 只做诊断，逐项说明 warn/fail 项的可能原因与影响程度；
2. 给出修复思路的文字性说明（描述步骤即可）；
3. 【禁止】输出任何 shell 命令、脚本、代码块、反引号内容；
4. 输出格式：分项分析 + 末尾总结建议。`, machine, score, overall, truncate(itemsJSON, 4000), truncate(snapJSON, 2000))
}

// callLLM 调用 OpenAI 兼容 /chat/completions。
func callLLM(baseURL, model, apiKey, prompt string, timeoutSec int) (string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是专业的服务器运维诊断助手，只输出分析文字，绝不输出任何命令。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3,
		"max_tokens":  2048,
	})
	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM 返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM 响应为空")
	}
	return out.Choices[0].Message.Content, nil
}

var cmdPattern = regexp.MustCompile(`(?m)^\s*(rm|sudo|systemctl|service|apt|apt-get|yum|dnf|pip|docker|kill|pkill|shutdown|reboot|dd|mkfs|fdisk|iptables|ufw|cscli|fail2ban-client|clamscan|chmod|chown|tar|wget|curl)\s|[\`+"`"+`]|\$\(`)

// containsCommandLike 启发式检测输出是否含疑似命令内容（仅用于标注"仅供参考"）。
func containsCommandLike(text string) bool {
	return cmdPattern.MatchString(text)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
