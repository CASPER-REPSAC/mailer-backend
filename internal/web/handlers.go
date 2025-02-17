package web

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// APIHandler는 템플릿, 사용자, 이메일 발송 등 API 엔드포인트에 필요한 의존성을 보관합니다.
type APIHandler struct {
	OIDCService     *auth.OIDCService
	TemplateManager *email.TemplateManager
	SMTPClient      *email.SMTPClient
	AuthentikClient *auth.AuthentikClient
}

// NewAPIHandler는 APIHandler 인스턴스를 생성합니다.
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
// 클라이언트가 수정한 템플릿 내용을 받아 샘플 데이터로 렌더링한 HTML을 반환합니다.
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
		"group": "테스터",
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

	successCount := 0
	// 각 수신자에 대해 템플릿 렌더링 후 개별 이메일 발송
	for _, rec := range reqData.Recipient {
		// 템플릿 내 플레이스홀더에 사용할 데이터 (예: {{.name}}, {{.email}}, {{.year}})
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

		go func() {
			if err := h.SMTPClient.SendEmail([]string{rec.Email}, reqData.Subject, body); err != nil {
				log.Printf("이메일 발송 실패 (%s): %v", rec.Email, err)
				return
			}
			log.Printf("이메일 발송 성공 (%s)", rec.Email)
		}()
		successCount++
	}

	response := map[string]interface{}{
		"message": fmt.Sprintf("총 %d명의 수신자에게 이메일 발송에 성공했습니다.", successCount),
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
