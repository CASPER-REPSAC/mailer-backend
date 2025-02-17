package web

import (
	"log"
	"net/http"
	"time"
)

// LoggingMiddleware 모든 요청에 대해 URL, 메서드 및 처리시간을 로깅합니다.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Started %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Completed %s in %v", r.URL.Path, time.Since(start))
	})
}

// RecoveryMiddleware panic 발생 시 이를 복구하고 500 에러를 반환합니다.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("Recovered from panic: %v", rec)
				http.Error(w, "내부 서버 오류가 발생했습니다.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
