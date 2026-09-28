package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Запуск HTTP сервера")
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("HTTP server error: %v", err)
		return

	}

}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
