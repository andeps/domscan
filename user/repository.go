package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var ErrEmailExists = errors.New("邮箱已注册")
var ErrInvalidCredentials = errors.New("邮箱或密码错误")

type User struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	Email        string    `json:"email" gorm:"not null;uniqueIndex"`
	PasswordHash string    `json:"-" gorm:"not null"`
	CreatedAt    time.Time `json:"createdAt"`
	Avatar       string    `json:"avatar,omitempty" gorm:"not null;default:''"`
}

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Migrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&User{})
}

func (r *Repository) Register(ctx context.Context, email, password string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{Email: normalizeEmail(email), PasswordHash: string(hash)}
	err = r.db.WithContext(ctx).Create(&u).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return User{}, ErrEmailExists
	}
	return u, err
}

func (r *Repository) UpdateEmail(ctx context.Context, current, next string) error {
	err := r.db.WithContext(ctx).Model(&User{}).
		Where("email = ?", normalizeEmail(current)).
		Update("email", normalizeEmail(next)).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrEmailExists
	}
	return err
}

func (r *Repository) UpdatePassword(ctx context.Context, email, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&User{}).
		Where("email = ?", normalizeEmail(email)).
		Update("password_hash", string(hash)).Error
}

func (r *Repository) UpdateAvatar(ctx context.Context, email, avatar string) error {
	return r.db.WithContext(ctx).Model(&User{}).
		Where("email = ?", normalizeEmail(email)).
		Update("avatar", avatar).Error
}

func (r *Repository) Authenticate(ctx context.Context, email, password string) (User, error) {
	var u User
	err := r.db.WithContext(ctx).Where("email = ?", normalizeEmail(email)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
