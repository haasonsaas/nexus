package config

import "time"

type AuthConfig struct {
	JWTSecret   string         `yaml:"jwt_secret"`
	TokenExpiry time.Duration  `yaml:"token_expiry"`
	APIKeys     []APIKeyConfig `yaml:"api_keys"`
	OAuth       OAuthConfig    `yaml:"oauth"`
}

type APIKeyConfig struct {
	Key    string `yaml:"key"`
	UserID string `yaml:"user_id"`
	Email  string `yaml:"email"`
	Name   string `yaml:"name"`
}

type OAuthConfig struct {
	Google OAuthProviderConfig `yaml:"google"`
	GitHub OAuthProviderConfig `yaml:"github"`
}

type OAuthProviderConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}
