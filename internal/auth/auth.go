package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"frpmon/internal/store"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	StatusApproved = "approved"
	StatusPending  = "pending"
)

var (
	ErrUsernameTaken   = errors.New("用户名已被占用")
	ErrWeakPassword    = errors.New("密码强度不足：至少 8 位且包含字母和数字")
	ErrInvalidCreds    = errors.New("用户名或密码错误")
	ErrAccountLocked   = errors.New("登录失败次数过多，已临时锁定")
	ErrPendingApproval = errors.New("账户待管理员审批")
	ErrTooManyUsers    = errors.New("注册已关闭，请联系管理员创建账户")
	ErrInvalidUsername = errors.New("用户名不能为空且不得超过 64 位，仅允许字母、数字、点、短横线和下划线")
)

type Service struct {
	db *store.DB
}

func NewService(db *store.DB) *Service {
	return &Service{db: db}
}

func (s *Service) DB() *store.DB { return s.db }

// FirstUserIsAdmin 首个注册用户自动成为管理员。
func (s *Service) IsFirstUser() (bool, error) {
	n, err := s.db.CountUsers()
	return n == 0, err
}

// Register 注册新账户。注册模式由 cfg 决定，返回创建的用户。
// mode: open | approval | closed（closed 时直接拒绝注册）
func (s *Service) Register(username, password, mode string) (*store.User, error) {
	username = strings.TrimSpace(username)
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	first, err := s.IsFirstUser()
	if err != nil {
		return nil, err
	}
	if !first && mode == "closed" {
		return nil, ErrTooManyUsers
	}
	role, status := RoleUser, StatusApproved
	if first {
		role, status = RoleAdmin, StatusApproved
	} else if mode == "approval" {
		status = StatusPending
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &store.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Status:       status,
		CreatedAt:    time.Now(),
	}
	if err := s.db.CreateUser(u); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return u, nil
}

func validatePassword(pw string) error {
	if len(pw) < 8 {
		return ErrWeakPassword
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrWeakPassword
	}
	return nil
}

func validateUsername(username string) error {
	if len(username) == 0 || len(username) > 64 {
		return ErrInvalidUsername
	}
	for _, r := range username {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			continue
		}
		return ErrInvalidUsername
	}
	return nil
}

// Login 登录：校验 bcrypt，并把账户与客户端地址的失败计数持久化到 DB。
// Login 保留给 CLI/内部调用；HTTP 层应使用 LoginWithClient，传入可信的
// RemoteAddr，从而避免攻击者通过不断更换用户名绕过账户锁定。
func (s *Service) Login(username, password string, sessionTTLDays, maxFails, lockMinutes int) (*store.User, string, error) {
	windowMinutes := lockMinutes * 2
	if windowMinutes < 15 {
		windowMinutes = 15
	}
	return s.LoginWithClient(username, password, sessionTTLDays, maxFails, maxFails*3, lockMinutes, windowMinutes, "")
}

// LoginWithClient 同时提供账户级和客户端级限流。clientKey 只应由服务端
// 从 RemoteAddr 提取，不直接信任可伪造的 X-Forwarded-For。
func (s *Service) LoginWithClient(username, password string, sessionTTLDays, maxFails, ipMaxFails, lockMinutes, windowMinutes int, clientKey string) (*store.User, string, error) {
	username = strings.TrimSpace(username)
	accountKey := "account:" + strings.ToLower(username)
	if limited, err := s.isLoginLimited(accountKey, "ip:"+clientKey); err != nil {
		return nil, "", err
	} else if limited {
		return nil, "", ErrAccountLocked
	}

	u, err := s.db.GetUserByName(username)
	// 对不存在的用户名也执行一次同成本 bcrypt，降低账号枚举的时序差异，
	// 同时避免原实现对 nil 用户解引用导致的 panic。
	hash := dummyPasswordHash
	if err == nil && u != nil {
		hash = u.PasswordHash
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if err != nil || u == nil || !valid {
		if lockErr := s.registerLoginFailure(accountKey, "ip:"+clientKey, maxFails, ipMaxFails, lockMinutes, windowMinutes); lockErr != nil {
			return nil, "", lockErr
		}
		return nil, "", ErrInvalidCreds
	}
	if err := s.clearLoginFailures(accountKey, "ip:"+clientKey); err != nil {
		return nil, "", err
	}
	if u.Status != StatusApproved {
		return nil, "", ErrPendingApproval
	}
	token, err := newToken()
	if err != nil {
		return nil, "", err
	}
	sess := &store.Session{
		Token:     token,
		UserID:    u.ID,
		ExpiresAt: time.Now().Add(time.Duration(sessionTTLDays) * 24 * time.Hour),
	}
	if err := s.db.CreateSession(sess); err != nil {
		return nil, "", err
	}
	s.db.UpdateUserLastLogin(u.ID)
	return u, token, nil
}

func (s *Service) isLoginLimited(accountKey, ipKey string) (bool, error) {
	for _, key := range []string{accountKey, ipKey} {
		if strings.HasSuffix(key, ":") {
			continue
		}
		limit, err := s.db.GetLoginLimit(key)
		if err != nil {
			return false, err
		}
		if limit != nil && limit.LockedUntil != nil && time.Now().Before(*limit.LockedUntil) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) registerLoginFailure(accountKey, ipKey string, maxFails, ipMaxFails, lockMinutes, windowMinutes int) error {
	if maxFails <= 0 {
		maxFails = 5
	}
	if ipMaxFails <= 0 {
		ipMaxFails = maxFails * 3
	}
	if lockMinutes <= 0 {
		lockMinutes = 10
	}
	if windowMinutes <= 0 {
		windowMinutes = 15
	}
	if _, err := s.db.RegisterLoginFailure(accountKey, maxFails,
		time.Duration(lockMinutes)*time.Minute, time.Duration(windowMinutes)*time.Minute); err != nil {
		return err
	}
	if !strings.HasSuffix(ipKey, ":") {
		if _, err := s.db.RegisterLoginFailure(ipKey, ipMaxFails,
			time.Duration(lockMinutes)*time.Minute, time.Duration(windowMinutes)*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) clearLoginFailures(accountKey, ipKey string) error {
	if err := s.db.ClearLoginLimit(accountKey); err != nil {
		return err
	}
	if !strings.HasSuffix(ipKey, ":") {
		return s.db.ClearLoginLimit(ipKey)
	}
	return nil
}

// Auth 校验会话 token，返回用户。失败返回 nil。
func (s *Service) Auth(token string) *store.User {
	if token == "" {
		return nil
	}
	sess, err := s.db.GetSession(token)
	if err != nil || time.Now().After(sess.ExpiresAt) {
		return nil
	}
	u, err := s.db.GetUserByID(sess.UserID)
	if err != nil || u.Status != StatusApproved {
		return nil
	}
	return u
}

func (s *Service) Logout(token string) error { return s.db.DeleteSession(token) }

// DeleteUser 销户：管理员可删任意账户（最后一个管理员禁止删除）；
// 普通用户只能删除自己（调用方需校验 session 归属）。
func (s *Service) DeleteUser(targetID, operatorID int64, operatorRole string) error {
	target, err := s.db.GetUserByID(targetID)
	if err != nil {
		return err
	}
	if target.Role == RoleAdmin {
		admins := 0
		users, err := s.db.ListUsers()
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Role == RoleAdmin {
				admins++
			}
		}
		if admins <= 1 && targetID == operatorID {
			return errors.New("不能删除最后一个管理员")
		}
	}
	if operatorRole != RoleAdmin && targetID != operatorID {
		return errors.New("无权限删除他人账户")
	}
	return s.db.DeleteUser(targetID)
}

// ApproveUser 审批注册（管理员）。
func (s *Service) ApproveUser(id int64, approve bool) error {
	if approve {
		return s.db.UpdateUserStatus(id, StatusApproved)
	}
	return s.db.DeleteUser(id)
}

func (s *Service) SetRole(id int64, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return errors.New("非法角色")
	}
	return s.db.UpdateUserRole(id, role)
}

// CheckPassword 修改密码前校验旧密码（供个人中心使用，M1 先提供基础接口）。
func (s *Service) VerifyPassword(u *store.User, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pw)) == nil
}

func (s *Service) SetPassword(id int64, pw string) error {
	if err := validatePassword(pw); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.UpdateUserPassword(id, string(hash))
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成会话 token 失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// 仅用于未知用户名的 bcrypt 比较，避免通过响应时间区分“用户名不存在”。
// 生成一次即可，成本与用户密码哈希一致。
var dummyPasswordHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("frpilot-invalid-login"), bcrypt.DefaultCost)
	if err != nil {
		return "$2a$10$7EqJtq98hPqEX7fNZaFWoO8Y8f3Qq4sB3l0oM1n1m4xY3x3Jw7m8K"
	}
	return string(h)
}()

// ConstantTimeEq 常量时间比较（防时序攻击，可用于 token 比对）。
func ConstantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
