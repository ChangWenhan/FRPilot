package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	ErrUsernameTaken = errors.New("用户名已被占用")
	ErrWeakPassword  = errors.New("密码强度不足：至少 8 位且包含字母和数字")
	ErrInvalidCreds  = errors.New("用户名或密码错误")
	ErrAccountLocked = errors.New("登录失败次数过多，已临时锁定")
	ErrPendingApproval = errors.New("账户待管理员审批")
	ErrTooManyUsers  = errors.New("注册已关闭，请联系管理员创建账户")
)

type Service struct {
	db *store.DB
	// 防爆破：key=username，记录失败次数与锁定时间
	mu    sync.Mutex
	fails map[string]*failEntry
}

type failEntry struct {
	count int
	until time.Time
}

func NewService(db *store.DB) *Service {
	return &Service{db: db, fails: map[string]*failEntry{}}
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

// Login 登录：校验 bcrypt + 防爆破（每用户名 5 次失败锁 10 分钟）。
func (s *Service) Login(username, password string, sessionTTLDays, maxFails, lockMinutes int) (*store.User, string, error) {
	username = strings.TrimSpace(username)
	s.mu.Lock()
	entry := s.fails[username]
	if entry != nil && time.Now().Before(entry.until) {
		s.mu.Unlock()
		return nil, "", ErrAccountLocked
	}
	s.mu.Unlock()

	u, err := s.db.GetUserByName(username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.registerFail(username, maxFails, lockMinutes)
		return nil, "", ErrInvalidCreds
	}
	s.clearFail(username)
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

func (s *Service) registerFail(username string, maxFails, lockMinutes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.fails[username]
	if e == nil {
		e = &failEntry{}
		s.fails[username] = e
	}
	e.count++
	if e.count >= maxFails {
		e.until = time.Now().Add(time.Duration(lockMinutes) * time.Minute)
		e.count = 0
	}
}

func (s *Service) clearFail(username string) {
	s.mu.Lock()
	delete(s.fails, username)
	s.mu.Unlock()
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

// ConstantTimeEq 常量时间比较（防时序攻击，可用于 token 比对）。
func ConstantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
