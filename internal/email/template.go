package email

import (
	"bytes"
	"fmt"
	"github.com/jordan-wright/email"
	"golang.org/x/crypto/sha3"
	"html/template"
	"net/textproto"
	"os"
	"path/filepath"
	"sync"
)

// TemplateManager manages the loading and rendering of HTML email templates.
type TemplateManager struct {
	templates map[string]*template.Template
	baseDir   string
	imageDir  string
	mu        sync.RWMutex
}

func NewTemplateManager(baseDir string, imageDir string) *TemplateManager {
	return &TemplateManager{
		templates: make(map[string]*template.Template),
		baseDir:   baseDir,
		imageDir:  imageDir,
	}
}

func (tm *TemplateManager) BaseDir() string {
	return tm.baseDir
}

// LoadTemplate loads a template file and caches it under the given name.
func (tm *TemplateManager) LoadTemplate(name, filename string) error {
	fullPath := filepath.Join(tm.baseDir, filename)
	tmpl, err := template.New(filename).Funcs(template.FuncMap{
		"image":         func(imageSrc string) string { return "" },
		"imageWithSize": func(imageSrc, width, height string) string { return "" },
		"property":      func(key string) string { return "" },
	}).ParseFiles(fullPath)
	if err != nil {
		return fmt.Errorf("failed to parse template file %s: %v", fullPath, err)
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.templates[name] = tmpl
	return nil
}

// RenderTemplate executes the cached template identified by name using the provided data.
func (tm *TemplateManager) RenderTemplate(name string, data interface{}) (string, []email.Attachment, error) {
	tm.mu.RLock()
	tmpl, exists := tm.templates[name]
	tm.mu.RUnlock()
	if !exists {
		return "", nil, fmt.Errorf("template %s not found", name)
	}
	var buf bytes.Buffer
	var attachments []email.Attachment
	tmpl = tmpl.Funcs(template.FuncMap{
		"image": func(imageSrc string) template.HTML {
			imagePath := filepath.Join(tm.imageDir, imageSrc)
			if _, err := os.Stat(imagePath); err != nil {
				return template.HTML(fmt.Sprintf("Image not found: %s", imageSrc))
			}
			content, err := os.ReadFile(imagePath)
			if err != nil {
				return template.HTML(fmt.Sprintf("Failed to read image: %s", imageSrc))
			}
			// Inline images must have unique names
			// 메일 읽지도 않았는데 탈락 이미지 이름이 미리보기에 나오면 슬프지 않을까요?
			ext := filepath.Ext(imageSrc)
			imageSrc = fmt.Sprintf("%s%s", hashString(imageSrc), ext)
			header := textproto.MIMEHeader{
				"Content-ID": {fmt.Sprintf("<%s>", imageSrc)},
			}
			attachments = append(attachments, email.Attachment{
				Filename:    imageSrc,
				Content:     content,
				HTMLRelated: true,
				ContentType: fmt.Sprintf("image/%s", filepath.Ext(imageSrc)[1:]),
				Header:      header,
			})
			return template.HTML(fmt.Sprintf("<img src=\"cid:%s\" alt=\"%s\">", imageSrc, imageSrc))
		},
		"imageWithSize": func(imageSrc, width, height string) template.HTML {
			imagePath := filepath.Join(tm.imageDir, imageSrc)
			if _, err := os.Stat(imagePath); err != nil {
				return template.HTML(fmt.Sprintf("Image not found: %s", imageSrc))
			}
			content, err := os.ReadFile(imagePath)
			if err != nil {
				return template.HTML(fmt.Sprintf("Failed to read image: %s", imageSrc))
			}
			// Inline images must have unique names
			// 메일 읽지도 않았는데 탈락 이미지 이름이 미리보기에 나오면 슬프지 않을까요?
			ext := filepath.Ext(imageSrc)
			imageSrc = fmt.Sprintf("%s-%s%s", hashString(imageSrc), width, ext)
			header := textproto.MIMEHeader{
				"Content-ID": {fmt.Sprintf("<%s>", imageSrc)},
			}
			attachments = append(attachments, email.Attachment{
				Filename:    imageSrc,
				Content:     content,
				HTMLRelated: true,
				ContentType: fmt.Sprintf("image/%s", filepath.Ext(imageSrc)[1:]),
				Header:      header,
			})
			return template.HTML(fmt.Sprintf("<img src=\"cid:%s\" alt=\"%s\" width=\"%s\" height=\"%s\">", imageSrc, imageSrc, width, height))
		},
		"property": func(key string) string {
			if dataMap, ok := data.(map[string]interface{}); ok {
				if custom, ok := dataMap["custom"].(map[string]string); ok {
					if val, exists := custom[key]; exists {
						return val
					}
				}
			}
			return ""
		},
	})

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", nil, fmt.Errorf("failed to execute template %s: %v", name, err)
	}
	return buf.String(), attachments, nil
}

func (tm *TemplateManager) ListTemplates() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	var names []string
	for name := range tm.templates {
		names = append(names, name)
	}
	return names
}

func (tm *TemplateManager) ExportedTemplates() map[string]*template.Template {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	templatesCopy := make(map[string]*template.Template, len(tm.templates))
	for key, tmpl := range tm.templates {
		templatesCopy[key] = tmpl
	}
	return templatesCopy
}

func (tm *TemplateManager) DeleteTemplate(name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.templates, name)
}

func (tm *TemplateManager) Templates() map[string]*template.Template {
	return tm.ExportedTemplates()
}

func hashString(s string) string {
	h := sha3.New512()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}
