package email

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"
	"sync"
)

// TemplateManager manages the loading and rendering of HTML email templates.
// 템플릿 파일은 지정한 baseDir 하위에 존재하며, 내부 플레이스홀더 (예: {{.name}}, {{.group}}, {{.email}})
// 의 치환 기능을 지원합니다.
type TemplateManager struct {
	templates map[string]*template.Template
	baseDir   string
	mu        sync.RWMutex
}

// NewTemplateManager initializes and returns a new TemplateManager with the provided base directory.
func NewTemplateManager(baseDir string) *TemplateManager {
	return &TemplateManager{
		templates: make(map[string]*template.Template),
		baseDir:   baseDir,
	}
}

func (tm *TemplateManager) BaseDir() string {
	return tm.baseDir
}

// LoadTemplate loads a template file (relative to baseDir) and caches it under the given name.
// 예: 이름 "default"로 "default.html" 파일을 로드하여 캐시에 저장합니다.
func (tm *TemplateManager) LoadTemplate(name, filename string) error {
	fullPath := filepath.Join(tm.baseDir, filename)
	tmpl, err := template.ParseFiles(fullPath)
	if err != nil {
		return fmt.Errorf("failed to parse template file %s: %v", fullPath, err)
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.templates[name] = tmpl
	return nil
}

// RenderTemplate executes the cached template identified by name using the provided data.
// 데이터는 예를 들어 map[string]interface{} 형태로 전달할 수 있습니다.
func (tm *TemplateManager) RenderTemplate(name string, data interface{}) (string, error) {
	tm.mu.RLock()
	tmpl, exists := tm.templates[name]
	tm.mu.RUnlock()
	if !exists {
		return "", fmt.Errorf("template %s not found", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %v", name, err)
	}
	return buf.String(), nil
}

// ListTemplates returns a slice of the names of the currently loaded templates.
func (tm *TemplateManager) ListTemplates() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	var names []string
	for name := range tm.templates {
		names = append(names, name)
	}
	return names
}

// ExportedTemplates returns a shallow copy of the internal template map.
// 외부에서는 이 복사본을 읽기 전용으로 활용할 수 있습니다.
func (tm *TemplateManager) ExportedTemplates() map[string]*template.Template {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	templatesCopy := make(map[string]*template.Template, len(tm.templates))
	for key, tmpl := range tm.templates {
		templatesCopy[key] = tmpl
	}
	return templatesCopy
}

// DeleteTemplate removes the template identified by name from the cache.
func (tm *TemplateManager) DeleteTemplate(name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.templates, name)
}

// Templates returns the internal template map.
// 이 메서드는 내부에서만 읽기 전용으로 사용합니다.
func (tm *TemplateManager) Templates() map[string]*template.Template {
	return tm.ExportedTemplates()
}
