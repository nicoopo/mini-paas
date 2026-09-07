package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

// DeployJob représente un déploiement en cours (ou terminé) et diffuse sa sortie en direct
// à tous les clients SSE connectés, tout en gardant un historique pour les abonnés tardifs.
type DeployJob struct {
	mu          sync.Mutex
	buf         bytes.Buffer
	subscribers map[chan []byte]struct{}
	Status      string // "running", "success", "failed"
	Err         string
	StartedAt   time.Time
	FinishedAt  time.Time
}

// Write permet d'utiliser le job comme io.Writer : chaque écriture (sortie de git/docker)
// est à la fois bufferisée (pour les abonnés tardifs) et diffusée aux abonnés déjà connectés.
func (j *DeployJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.buf.Write(p)
	chunk := append([]byte(nil), p...)
	for ch := range j.subscribers {
		select {
		case ch <- chunk:
		default: // abonné trop lent : on ne bloque pas le déploiement pour lui
		}
	}
	return len(p), nil
}

// Subscribe renvoie le buffer déjà accumulé et, si le job tourne encore, un canal recevant
// la suite en direct. Le canal est nil si le job est déjà terminé : rien de plus à diffuser.
func (j *DeployJob) Subscribe() (ch chan []byte, backlog []byte, status string, unsubscribe func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	backlog = append([]byte(nil), j.buf.Bytes()...)
	status = j.Status
	if j.Status == "running" {
		ch = make(chan []byte, 64)
		j.subscribers[ch] = struct{}{}
	}
	return ch, backlog, status, func() {
		if ch == nil {
			return
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		delete(j.subscribers, ch)
	}
}

// Snapshot renvoie le statut courant du job, de façon thread-safe.
func (j *DeployJob) Snapshot() (status, errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, j.Err
}

func (j *DeployJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.FinishedAt = time.Now()
	if err != nil {
		j.Status = "failed"
		j.Err = err.Error()
	} else {
		j.Status = "success"
	}
	for ch := range j.subscribers {
		close(ch)
	}
	j.subscribers = map[chan []byte]struct{}{}
}

// JobManager garde le dernier job de déploiement par projet, en mémoire.
// ponytail: perdu au redémarrage du process, ce qui est acceptable — un déploiement en cours
// ne survivrait de toute façon pas à un arrêt du binaire.
type JobManager struct {
	mu   sync.Mutex
	jobs map[string]*DeployJob
}

func NewJobManager() *JobManager {
	return &JobManager{jobs: map[string]*DeployJob{}}
}

// Start lance fn en arrière-plan dans un nouveau job, sauf si un déploiement tourne déjà pour ce projet.
func (m *JobManager) Start(projectID string, fn func(w io.Writer) error) (*DeployJob, error) {
	m.mu.Lock()
	if existing, ok := m.jobs[projectID]; ok {
		if status, _ := existing.Snapshot(); status == "running" {
			m.mu.Unlock()
			return nil, fmt.Errorf("un déploiement est déjà en cours pour ce projet")
		}
	}
	job := &DeployJob{Status: "running", StartedAt: time.Now(), subscribers: map[chan []byte]struct{}{}}
	m.jobs[projectID] = job
	m.mu.Unlock()

	go func() {
		job.finish(fn(job))
	}()
	return job, nil
}

func (m *JobManager) Get(projectID string) (*DeployJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[projectID]
	return job, ok
}
