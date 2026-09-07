package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Project est un dépôt Git qu'on sait (re)déployer dans un conteneur Docker.
type Project struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	RepoURL       string     `json:"repoUrl"`
	Branch        string     `json:"branch,omitempty"` // vide = branche par défaut du remote
	EnvVars       []string   `json:"envVars,omitempty"` // chaque entrée au format "KEY=VALUE"
	HostPort      string     `json:"hostPort"`
	ContainerPort string     `json:"containerPort"`
	LastDeployAt  *time.Time `json:"lastDeployAt,omitempty"`
}

func (p Project) ContainerName() string { return "paas-" + p.ID }

// Store persiste la liste des projets dans un simple fichier JSON.
// ponytail: pas de base de données pour une poignée de projets perso, un fichier JSON + mutex suffit.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) load() ([]Project, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []Project{}, nil
	}
	if err != nil {
		return nil, err
	}
	var projects []Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (s *Store) save(projects []Project) error {
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *Store) List() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) Get(id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.load()
	if err != nil {
		return Project{}, err
	}
	for _, p := range projects {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, fmt.Errorf("projet %q introuvable", id)
}

func (s *Store) Add(p Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.load()
	if err != nil {
		return err
	}
	projects = append(projects, p)
	return s.save(projects)
}

func (s *Store) Update(p Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.load()
	if err != nil {
		return err
	}
	for i, existing := range projects {
		if existing.ID == p.ID {
			projects[i] = p
			return s.save(projects)
		}
	}
	return fmt.Errorf("projet %q introuvable", p.ID)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.load()
	if err != nil {
		return err
	}
	out := projects[:0]
	for _, p := range projects {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return s.save(out)
}
