package user

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Handler struct {
	repo   *Repository
	secret []byte
}

const EmailContextKey = "userEmail"

func NewHandler(repo *Repository, secret string) *Handler {
	return &Handler{repo: repo, secret: []byte(secret)}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Register(c *gin.Context) {
	var in credentials
	if c.ShouldBindJSON(&in) != nil || !validEmail(in.Email) || len(in.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效邮箱和至少 8 位密码"})
		return
	}
	u, err := h.repo.Register(c.Request.Context(), in.Email, in.Password)
	if errors.Is(err, ErrEmailExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "邮箱已注册"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u})
}
func (h *Handler) Login(c *gin.Context) {
	var in credentials
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	u, err := h.repo.Authenticate(c.Request.Context(), in.Email, in.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "邮箱或密码错误"})
		return
	}
	token := h.issueToken(u.Email)
	c.JSON(http.StatusOK, gin.H{"user": u, "token": token, "message": "登录成功"})
}

func (h *Handler) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, jwt.ErrSignatureInvalid
			}
			return h.secret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		email, ok2 := claims["sub"].(string)
		if !ok || !ok2 || email == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Token 无效"})
			return
		}
		c.Set(EmailContextKey, email)
		c.Next()
	}
}
func EmailFromContext(c *gin.Context) string {
	return c.GetString(EmailContextKey)
}

type updateEmailRequest struct {
	NewEmail string `json:"newEmail"`
}

type updatePasswordRequest struct {
	Password string `json:"password"`
}

type updateAvatarRequest struct {
	Avatar string `json:"avatar"`
}

func (h *Handler) UpdateEmail(c *gin.Context) {
	var in updateEmailRequest
	if c.ShouldBindJSON(&in) != nil || !validEmail(in.NewEmail) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效的新邮箱"})
		return
	}
	if err := h.repo.UpdateEmail(c.Request.Context(), EmailFromContext(c), in.NewEmail); errors.Is(err, ErrEmailExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "邮箱已被占用"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "邮箱修改失败"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.NewEmail))
	c.JSON(http.StatusOK, gin.H{"email": email, "token": h.issueToken(email)})
}
func (h *Handler) UpdatePassword(c *gin.Context) {
	var in updatePasswordRequest
	if c.ShouldBindJSON(&in) != nil || len(in.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "密码至少 8 位"})
		return
	}
	if err := h.repo.UpdatePassword(c.Request.Context(), EmailFromContext(c), in.Password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码修改失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码已修改"})
}
func (h *Handler) UpdateAvatar(c *gin.Context) {
	var in updateAvatarRequest
	if c.ShouldBindJSON(&in) != nil || len(in.Avatar) > 2*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "头像无效或超过 2MB"})
		return
	}
	if err := h.repo.UpdateAvatar(c.Request.Context(), EmailFromContext(c), in.Avatar); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "头像保存失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"avatar": in.Avatar})
}

func (h *Handler) issueToken(email string) string {
	now := time.Now()
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": email,
		"iat": now.Unix(),
		"exp": now.Add(7 * 24 * time.Hour).Unix(),
	}).SignedString(h.secret)
	return token
}

func validEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && !strings.ContainsAny(email, "\r\n")
}
