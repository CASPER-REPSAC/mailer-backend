package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/vanng822/go-premailer/premailer"
	"html/template"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"mail-manager/internal/auth"
	"mail-manager/internal/email"
)

type APIHandler struct {
	OIDCService     *auth.OIDCService
	TemplateManager *email.TemplateManager
	SMTPClient      *email.SMTPClient
	AuthentikClient *auth.AuthentikClient
}

func NewAPIHandler(oidc *auth.OIDCService, tm *email.TemplateManager, smtp *email.SMTPClient, authClient *auth.AuthentikClient) *APIHandler {
	return &APIHandler{
		OIDCService:     oidc,
		TemplateManager: tm,
		SMTPClient:      smtp,
		AuthentikClient: authClient,
	}
}

// MeHandler handles GET /api/me by returning the current logged-in user's info.
func (h *APIHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}
	session, err := h.OIDCService.Store.Get(r, "oidc-session")
	if err != nil {
		http.Error(w, "세션을 불러오는 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	idToken, ok := session.Values["id_token"].(string)
	if !ok || idToken == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "로그인되지 않았습니다."})
		return
	}
	emailAddress, _ := session.Values["email"].(string)
	name, _ := session.Values["name"].(string)
	response := map[string]string{
		"email": emailAddress,
		"name":  name,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// TemplatesListHandler handles GET /api/templates to list loaded template names.
func (h *APIHandler) TemplatesListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}
	templates := h.TemplateManager.ListTemplates()
	response := map[string]interface{}{
		"templates": templates,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *APIHandler) TemplateHandler(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/templates/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "잘못된 URL 경로입니다.", http.StatusBadRequest)
		return
	}
	tmplName := strings.TrimPrefix(r.URL.Path, prefix)
	tmplName = path.Clean(tmplName)
	if tmplName == "." || tmplName == "/" || tmplName == "" {
		http.Error(w, "유효한 템플릿 이름이 제공되지 않았습니다.", http.StatusBadRequest)
		return
	}
	if strings.Contains(tmplName, "/") {
		http.Error(w, "템플릿 이름에 유효하지 않은 문자가 포함되어 있습니다.", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		filePath := filepath.Join(h.TemplateManager.BaseDir(), tmplName+".html")
		content, err := os.ReadFile(filePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿을 찾을 수 없습니다: %s", tmplName), http.StatusNotFound)
			return
		}
		response := map[string]interface{}{
			"name":    tmplName,
			"content": string(content),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)

	case http.MethodPost:
		var reqData struct {
			Content string `json:"content"`
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			mediatype, _, _ := mime.ParseMediaType(ct)
			if mediatype != "application/json" {
				http.Error(w, "Content-Type은 application/json 이어야 합니다.", http.StatusBadRequest)
				return
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
			http.Error(w, "올바르지 않은 JSON 페이로드입니다.", http.StatusBadRequest)
			return
		}
		if reqData.Content == "" {
			http.Error(w, "템플릿 내용이 비어 있습니다.", http.StatusBadRequest)
			return
		}
		filePath := filepath.Join(h.TemplateManager.BaseDir(), tmplName+".html")
		if err := os.WriteFile(filePath, []byte(reqData.Content), 0644); err != nil {
			http.Error(w, "템플릿 저장에 실패하였습니다.", http.StatusInternalServerError)
			log.Printf("템플릿 %s 업데이트 오류: %v", tmplName, err)
			return
		}
		if err := h.TemplateManager.LoadTemplate(tmplName, tmplName+".html"); err != nil {
			http.Error(w, "템플릿 캐시 갱신에 실패하였습니다.", http.StatusInternalServerError)
			log.Printf("템플릿 %s 캐시 갱신 오류: %v", tmplName, err)
			return
		}
		response := map[string]interface{}{
			"message": "템플릿이 정상적으로 업데이트되었습니다.",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)

	case http.MethodDelete:
		if tmplName == "default" {
			http.Error(w, "기본 템플릿은 삭제할 수 없습니다.", http.StatusBadRequest)
			return
		}
		filePath := filepath.Join(h.TemplateManager.BaseDir(), tmplName+".html")
		if err := os.Remove(filePath); err != nil {
			http.Error(w, fmt.Sprintf("템플릿 삭제에 실패하였습니다: %v", err), http.StatusInternalServerError)
			return
		}
		// 캐시에서도 제거
		h.TemplateManager.DeleteTemplate(tmplName)
		response := map[string]interface{}{
			"message": "템플릿이 삭제되었습니다.",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
	}
}

// PreviewTemplateHandler handles POST /api/templates/preview/{name}.
func (h *APIHandler) PreviewTemplateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/api/templates/preview/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "잘못된 URL 경로입니다.", http.StatusBadRequest)
		return
	}
	tmplName := strings.TrimPrefix(r.URL.Path, prefix)
	tmplName = path.Clean(tmplName)
	if tmplName == "" || tmplName == "." || tmplName == "/" {
		http.Error(w, "유효한 템플릿 이름이 제공되지 않았습니다.", http.StatusBadRequest)
		return
	}

	var reqData struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "올바르지 않은 JSON 페이로드입니다.", http.StatusBadRequest)
		return
	}

	var tpl *template.Template
	var err error
	if reqData.Content != "" {
		tpl, err = template.New(tmplName).Parse(reqData.Content)
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿 파싱 실패: %v", err), http.StatusBadRequest)
			return
		}
	} else {
		// 내용이 제공되지 않으면, 기존 파일을 사용합니다.
		filePath := filepath.Join(h.TemplateManager.BaseDir(), tmplName+".html")
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿을 찾을 수 없습니다: %s", tmplName), http.StatusNotFound)
			return
		}
		tpl, err = template.New(tmplName).Parse(string(contentBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿 파싱 실패: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// 미리보기용 샘플 데이터(요구사항에 따른 플레이스홀더 치환)
	sampleData := map[string]interface{}{
		"name":  "홍길동",
		"email": "test@example.com",
		"year":  2025,
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, sampleData); err != nil {
		http.Error(w, fmt.Sprintf("템플릿 렌더링 실패: %v", err), http.StatusInternalServerError)
		return
	}
	renderedHTML := buf.String()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	premail, err := premailer.NewPremailerFromString(renderedHTML, premailer.NewOptions())
	if err != nil {
		http.Error(w, fmt.Sprintf("프리메일러 생성 실패: %v", err), http.StatusInternalServerError)
		return
	}
	renderedHTML, err = premail.Transform()
	if err != nil {
		http.Error(w, fmt.Sprintf("프리메일러 변환 실패: %v", err), http.StatusInternalServerError)
	}
	_, _ = w.Write([]byte(renderedHTML))
}

// UsersHandler handles GET /api/users to return the list of users.
func (h *APIHandler) UsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	users, err := h.AuthentikClient.GetUserList(ctx)
	if err != nil {
		http.Error(w, "사용자 목록 조회에 실패하였습니다.", http.StatusInternalServerError)
		log.Printf("UsersHandler 오류: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

// EmailHandler handles POST /api/email to send emails.
func (h *APIHandler) EmailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	// RecipientInfo 구조체: 각 수신자의 이름과 이메일을 받습니다.
	type RecipientInfo struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	// JSON 페이로드 구조 정의
	var reqData struct {
		Template  string          `json:"template"`
		Subject   string          `json:"subject"`
		Recipient []RecipientInfo `json:"recipient"`
	}

	// JSON 디코딩
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "올바르지 않은 JSON 페이로드입니다.", http.StatusBadRequest)
		return
	}

	// 필수 필드 검증
	if reqData.Template == "" || reqData.Subject == "" || len(reqData.Recipient) == 0 {
		http.Error(w, "필수 필드가 누락되었습니다.", http.StatusBadRequest)
		return
	}
	go func(recipients []RecipientInfo) {
		for _, rec := range recipients {
			data := map[string]interface{}{
				"name":  rec.Name,
				"email": rec.Email,
				"year":  time.Now().Year(),
			}

			body, err := h.TemplateManager.RenderTemplate(reqData.Template, data)
			if err != nil {
				log.Printf("템플릿 렌더링 실패 (%s): %v", rec.Email, err)
				continue
			}
			premail, err := premailer.NewPremailerFromString(body, premailer.NewOptions())
			if err != nil {
				log.Printf("프리메일러 생성 실패 (%s): %v", rec.Email, err)
				continue
			}
			html, err := premail.Transform()
			if err != nil {
				log.Printf("프리메일러 변환 실패 (%s): %v", rec.Email, err)
			}
			for i := 0; i < 10; i++ {
				time.Sleep(10 * time.Second)
				if err := h.SMTPClient.SendEmail([]string{rec.Email}, reqData.Subject, html); err != nil {
					log.Printf("이메일 발송 실패 (%s): %v", rec.Email, err)
					continue
				}
				break
			}
			log.Printf("이메일 발송 성공 (%s)", rec.Email)
		}
	}(reqData.Recipient)

	response := map[string]interface{}{
		"message": fmt.Sprintf("총 %d명의 수신자에게 이메일을 발송합니다.", len(reqData.Recipient)),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// LogoutHandler handles POST /logout to clear the user session.
func (h *APIHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}
	session, err := h.OIDCService.Store.Get(r, "oidc-session")
	if err != nil {
		http.Error(w, "세션을 불러오는 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		http.Error(w, "로그아웃 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "로그아웃 되었습니다."})
}
