package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vanng822/go-premailer/premailer"
	"mail-manager/internal/auth"
	"mail-manager/internal/email"
)

type APIHandler struct {
	OIDCService     *auth.OIDCService
	TemplateManager *email.TemplateManager
	SMTPClient      *email.SMTPClient
	AuthentikClient *auth.AuthentikClient
	ImageDir        string
}

func NewAPIHandler(oidc *auth.OIDCService, tm *email.TemplateManager, smtp *email.SMTPClient, authClient *auth.AuthentikClient, imageDir string) *APIHandler {
	return &APIHandler{
		OIDCService:     oidc,
		TemplateManager: tm,
		SMTPClient:      smtp,
		AuthentikClient: authClient,
		ImageDir:        imageDir,
	}
}

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
		tpl, err = template.New(tmplName).Funcs(template.FuncMap{
			"image": func(imageSrc string) template.HTML {
				return template.HTML(fmt.Sprintf("<img src=\"%s\" style=\"max-width: 100%%; height: auto;\">",
					html.EscapeString("/api/images/"+imageSrc)))
			},
			"imageWithSize": func(imageSrc, width, height string) template.HTML {
				return template.HTML(fmt.Sprintf("<img src=\"%s\" width=\"%s\" height=\"%s\">",
					html.EscapeString("/api/images/"+imageSrc), html.EscapeString(width), html.EscapeString(height)))
			},
			"property": func(key string) template.HTML {
				return template.HTML(fmt.Sprintf("<code>PROPERTY(%s)</code>", key))
			},
		}).Parse(reqData.Content)
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿 파싱 실패: %v", err), http.StatusBadRequest)
			return
		}
	} else {
		filePath := filepath.Join(h.TemplateManager.BaseDir(), tmplName+".html")
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿을 찾을 수 없습니다: %s", tmplName), http.StatusNotFound)
			return
		}
		tpl, err = template.New(tmplName).Funcs(template.FuncMap{
			"image": func(imageSrc string) template.HTML {
				return template.HTML(fmt.Sprintf("<img src=\"%s\" alt=\"\" style=\"max-width: 100%%; height: auto;\">",
					html.EscapeString("/api/images/"+imageSrc)))
			},
			"imageWithSize": func(imageSrc, width, height string) template.HTML {
				return template.HTML(fmt.Sprintf("<img src=\"%s\" width=\"%s\" height=\"%s\">",
					html.EscapeString("/api/images/"+imageSrc), html.EscapeString(width), html.EscapeString(height)))
			},
			"property": func(key string) template.HTML {
				return template.HTML(fmt.Sprintf("<code>PROPERTY(%s)</code>", key))
			},
		}).Parse(string(contentBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("템플릿 파싱 실패: %v", err), http.StatusInternalServerError)
			return
		}
	}

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
		return
	}
	_, _ = w.Write([]byte(renderedHTML))
}

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

func (h *APIHandler) EmailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원하지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	type RecipientInfo struct {
		Name   string            `json:"name"`
		Email  string            `json:"email"`
		Custom map[string]string `json:"custom,omitempty"`
	}

	var reqData struct {
		Template  string          `json:"template"`
		Subject   string          `json:"subject"`
		Recipient []RecipientInfo `json:"recipient"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "올바르지 않은 JSON 페이로드입니다.", http.StatusBadRequest)
		return
	}

	if reqData.Template == "" || reqData.Subject == "" || len(reqData.Recipient) == 0 {
		http.Error(w, "필수 필드가 누락되었습니다.", http.StatusBadRequest)
		return
	}
	go func(recipients []RecipientInfo) {
		for _, rec := range recipients {
			data := map[string]interface{}{
				"name":   rec.Name,
				"email":  rec.Email,
				"year":   time.Now().Year(),
				"custom": rec.Custom,
			}
			body, attachments, err := h.TemplateManager.RenderTemplate(reqData.Template, data)
			if err != nil {
				log.Printf("템플릿 렌더링 실패 (%s): %v", rec.Email, err)
				continue
			}
			premail, err := premailer.NewPremailerFromString(body, premailer.NewOptions())
			if err != nil {
				log.Printf("프리메일러 생성 실패 (%s): %v", rec.Email, err)
				continue
			}
			htm, err := premail.Transform()
			if err != nil {
				log.Printf("프리메일러 변환 실패 (%s): %v", rec.Email, err)
			}
			for i := 0; i < 10; i++ {
				if err := h.SMTPClient.SendEmail([]string{rec.Email}, reqData.Subject, htm, attachments); err != nil {
					log.Printf("이메일 발송 실패 (%s): %v", rec.Email, err)
					time.Sleep(10 * time.Second)
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

func (h *APIHandler) ImageUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	file, handler, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	defer file.Close()
	allowed := map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/bmp": true}
	if !allowed[handler.Header.Get("Content-Type")] {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	if strings.Contains(handler.Filename, "/") || handler.Filename == "upload" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	imagePath := filepath.Join(h.ImageDir, handler.Filename)
	out, err := os.Create(imagePath)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, file)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "uploaded", "url": "/api/images/" + handler.Filename})
}

func (h *APIHandler) ImageServeHandler(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/images/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	filename := strings.TrimPrefix(r.URL.Path, prefix)
	filename = path.Clean(filename)
	if filename == "" || filename == "." || filename == "/" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	imagePath := filepath.Join(h.ImageDir, filename)
	http.ServeFile(w, r, imagePath)
}

func (h *APIHandler) ImageListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	files, err := os.ReadDir(h.ImageDir)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	var imgs []string
	allowedExt := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name()))
		if allowedExt[ext] {
			imgs = append(imgs, f.Name())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"images": imgs})
}

func (h *APIHandler) ImageDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/api/images/"
	filename := strings.TrimPrefix(r.URL.Path, prefix)
	filename = path.Clean(filename)
	if filename == "" || filename == "." || filename == "/" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(h.ImageDir, filename)
	if err := os.Remove(filePath); err != nil {
		http.Error(w, fmt.Sprintf("error: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "deleted"})
}
