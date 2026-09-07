package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

var store *Store

type projectView struct {
	Project
	Status string `json:"status"`
}

func main() {
	if err := os.MkdirAll("data/repos", 0o755); err != nil {
		log.Fatal(err)
	}
	store = NewStore("data/projects.json")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /api/projects", handleListProjects)
	mux.HandleFunc("POST /api/projects", handleCreateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", handleDeleteProject)
	mux.HandleFunc("POST /api/projects/{id}/deploy", handleDeploy)
	mux.HandleFunc("POST /api/projects/{id}/stop", handleStop)
	mux.HandleFunc("GET /api/projects/{id}/logs", handleLogs)

	password := os.Getenv("PAAS_PASSWORD")
	if password == "" {
		password = randomPassword()
		log.Printf("PAAS_PASSWORD non défini : mot de passe généré pour cette session -> %s", password)
		log.Printf("(fixe PAAS_PASSWORD pour garder le même mot de passe entre les redémarrages)")
	}

	addr := "127.0.0.1:8090"
	log.Printf("mini-PaaS sur http://%s (Ctrl+C pour arrêter)", addr)
	log.Fatal(http.ListenAndServe(addr, requireAuth(password, mux)))
}

// requireAuth protège tout le dashboard par HTTP Basic Auth avec un mot de passe partagé.
// L'utilisateur (login) n'est pas vérifié : un seul secret pour un outil mono-utilisateur.
func requireAuth(password string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="mini-paas"`)
			http.Error(w, "Authentification requise", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]projectView, 0, len(projects))
	for _, p := range projects {
		views = append(views, projectView{Project: p, Status: Status(p)})
	}
	writeJSON(w, views)
}

func handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repoUrl"`
		Branch        string `json:"branch"`
		EnvVars       string `json:"envVars"` // brut, une variable KEY=VALUE par ligne
		HostPort      string `json:"hostPort"`
		ContainerPort string `json:"containerPort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "JSON invalide", http.StatusBadRequest)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.Branch = strings.TrimSpace(in.Branch)
	in.HostPort = strings.TrimSpace(in.HostPort)
	if in.Name == "" || in.RepoURL == "" || in.HostPort == "" {
		http.Error(w, "name, repoUrl et hostPort sont requis", http.StatusBadRequest)
		return
	}
	envVars, err := parseEnvVars(in.EnvVars)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	p := Project{
		ID:            randomID(),
		Name:          in.Name,
		RepoURL:       in.RepoURL,
		Branch:        in.Branch,
		EnvVars:       envVars,
		HostPort:      in.HostPort,
		ContainerPort: in.ContainerPort,
	}
	if err := store.Add(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, p)
}

func handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_, _ = runCmd(".", "docker", "rm", "-f", p.ContainerName())
	_, _ = runCmd(".", "rm", "-rf", repoDir(p))
	if err := store.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeploy est synchrone : la requête reste ouverte le temps du clone+build+run.
// ponytail: pour un projet perso ça reste supportable ; passer à un job async + SSE si les builds deviennent longs.
func handleDeploy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	output, deployErr := Deploy(p)
	if deployErr == nil {
		now := time.Now()
		p.LastDeployAt = &now
		_ = store.Update(p)
	}

	status := "ok"
	if deployErr != nil {
		status = deployErr.Error()
	}
	writeJSON(w, map[string]string{"output": output, "status": status})
}

func handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	output, stopErr := Stop(p)
	status := "ok"
	if stopErr != nil {
		status = stopErr.Error()
	}
	writeJSON(w, map[string]string{"output": output, "status": status})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := store.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	output, _ := Logs(p)
	writeJSON(w, map[string]string{"output": output})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// parseEnvVars découpe un bloc "KEY=VALUE" ligne par ligne (lignes vides et # ignorées),
// et rejette toute ligne qui ne respecte pas ce format.
func parseEnvVars(raw string) ([]string, error) {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("variable d'environnement invalide (attendu KEY=VALUE): %q", line)
		}
		out = append(out, line)
	}
	return out, nil
}

func randomID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
