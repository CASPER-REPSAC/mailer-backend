package main

import (
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-manager/internal/auth"
	"mail-manager/internal/email"
	"mail-manager/internal/web"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`
	OIDC struct {
		ProviderURL  string   `yaml:"provider_url"`
		ClientID     string   `yaml:"client_id"`
		ClientSecret string   `yaml:"client_secret"`
		RedirectURL  string   `yaml:"redirect_url"`
		Scopes       []string `yaml:"scopes"`
	} `yaml:"oidc"`
	Authentik struct {
		BaseURL  string `yaml:"base_url"`
		ApiToken string `yaml:"api_token"`
	} `yaml:"authentik"`
	SMTP struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
		From     string `yaml:"from"`
	} `yaml:"smtp"`
	Templates struct {
		Email string `yaml:"email"`
		Image string `yaml:"image"`
	} `yaml:"templates"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func main() {
	configFile := flag.String("config", "config/config.yaml", "path to the configuration file")
	flag.Parse()

	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	log.Printf("Configuration loaded: server will listen on %s", cfg.Server.Address)

	if os.Getenv("SESSION_KEY") == "" {
		log.Fatalf("환경변수 SESSION_KEY가 설정되어 있지 않습니다")
	}

	oidcSvc, err := auth.NewOIDCService(&auth.OIDCConfig{
		ProviderURL:  cfg.OIDC.ProviderURL,
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURL,
		Scopes:       cfg.OIDC.Scopes,
	})
	if err != nil {
		log.Fatalf("OIDC service 초기화 실패: %v", err)
	}
	log.Println("OIDC service initialized.")

	authClient, err := auth.NewAuthentikClient(&auth.AuthentikConfig{
		BaseURL:  cfg.Authentik.BaseURL,
		ApiToken: cfg.Authentik.ApiToken,
	})
	if err != nil {
		log.Fatalf("Authentik client 초기화 실패: %v", err)
	}
	log.Println("Authentik client initialized.")

	smtpClient := email.NewSMTPClient(email.SMTPConfig{
		Host:     cfg.SMTP.Host,
		Port:     cfg.SMTP.Port,
		Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password,
		From:     cfg.SMTP.From,
	})
	log.Println("SMTP client initialized.")

	tmplManager := email.NewTemplateManager(cfg.Templates.Email, cfg.Templates.Image)
	files, err := os.ReadDir(cfg.Templates.Email)
	if err != nil {
		log.Fatalf("템플릿 디렉터리 읽기 실패: %v", err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if err := tmplManager.LoadTemplate(strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())), file.Name()); err != nil {
			log.Printf("템플릿 로드 실패: %v", err)
		}
	}

	apiHandler := web.NewAPIHandler(oidcSvc, tmplManager, smtpClient, authClient, cfg.Templates.Image)
	mux := http.NewServeMux()

	mux.HandleFunc("/login", oidcSvc.LoginHandler)
	mux.HandleFunc("/login/callback", oidcSvc.CallbackHandler)

	mux.Handle("/api/templates", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.TemplatesListHandler)))
	mux.Handle("/api/templates/", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.TemplateHandler)))
	mux.Handle("/api/templates/preview/", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.PreviewTemplateHandler)))
	mux.Handle("/api/users", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.UsersHandler)))
	mux.Handle("/api/email", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.EmailHandler)))
	mux.Handle("/api/me", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.MeHandler)))
	mux.Handle("/api/images/upload", oidcSvc.AuthMiddleware(http.HandlerFunc(apiHandler.ImageUploadHandler)))
	mux.Handle("/api/images/", oidcSvc.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			apiHandler.ImageDeleteHandler(w, r)
		} else if r.Method == http.MethodGet && r.URL.Path == "/api/images/" {
			apiHandler.ImageListHandler(w, r)
		} else if r.Method == http.MethodGet {
			apiHandler.ImageServeHandler(w, r)
		} else {
			http.Error(w, "", http.StatusMethodNotAllowed)
		}
	})))
	mux.Handle("/logout", http.HandlerFunc(apiHandler.LogoutHandler))

	// 전체 핸들러에 로깅 및 복구 미들웨어 적용
	finalHandler := web.RecoveryMiddleware(web.LoggingMiddleware(mux))

	server := &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      finalHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("서버 시작: %s", cfg.Server.Address)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("서버 종료: %v", err)
	}
}
