package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runCmd exécute une commande, capture stdout+stderr, et renvoie le tout même en cas d'erreur
// (utile pour afficher la sortie de build/déploiement dans le dashboard).
func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func repoDir(p Project) string {
	return filepath.Join("data", "repos", p.ID)
}

// cloneOrPull clone le repo s'il n'existe pas encore, sinon fait un pull.
// ponytail: toujours sur la branche par défaut du remote, pas de choix de branche pour le MVP.
func cloneOrPull(p Project) (string, error) {
	dir := repoDir(p)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return runCmd(dir, "git", "pull")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	return runCmd(".", "git", "clone", p.RepoURL, dir)
}

// dockerBuild suppose un Dockerfile à la racine du repo, comme n'importe quel PaaS basé sur Docker.
func dockerBuild(p Project) (string, error) {
	dir := repoDir(p)
	return runCmd(dir, "docker", "build", "-t", p.ContainerName(), ".")
}

// dockerRun retire l'ancien conteneur s'il existe puis relance l'image fraîchement buildée.
func dockerRun(p Project) (string, error) {
	name := p.ContainerName()
	var out bytes.Buffer

	if removeOut, _ := runCmd(".", "docker", "rm", "-f", name); removeOut != "" {
		out.WriteString(removeOut)
		out.WriteString("\n")
	}

	containerPort := p.ContainerPort
	if containerPort == "" {
		containerPort = "80"
	}
	portMap := fmt.Sprintf("%s:%s", p.HostPort, containerPort)

	runOut, err := runCmd(".", "docker", "run", "-d", "--name", name, "-p", portMap, name)
	out.WriteString(runOut)
	return out.String(), err
}

// Deploy enchaîne clone/pull -> build -> run, en s'arrêtant à la première étape qui échoue.
func Deploy(p Project) (string, error) {
	var log bytes.Buffer

	log.WriteString("$ git clone/pull\n")
	gitOut, err := cloneOrPull(p)
	log.WriteString(gitOut)
	if err != nil {
		return log.String(), fmt.Errorf("git a échoué: %w", err)
	}

	log.WriteString("\n$ docker build\n")
	buildOut, err := dockerBuild(p)
	log.WriteString(buildOut)
	if err != nil {
		return log.String(), fmt.Errorf("docker build a échoué: %w", err)
	}

	log.WriteString("\n$ docker run\n")
	runOut, err := dockerRun(p)
	log.WriteString(runOut)
	if err != nil {
		return log.String(), fmt.Errorf("docker run a échoué: %w", err)
	}

	return log.String(), nil
}

func Stop(p Project) (string, error) {
	return runCmd(".", "docker", "stop", p.ContainerName())
}

func Logs(p Project) (string, error) {
	return runCmd(".", "docker", "logs", "--tail", "200", p.ContainerName())
}

// Status renvoie l'état du conteneur ("running", "exited", ...) ou "non déployé" s'il n'existe pas.
func Status(p Project) string {
	out, err := runCmd(".", "docker", "inspect", "-f", "{{.State.Status}}", p.ContainerName())
	if err != nil {
		return "non déployé"
	}
	return strings.TrimSpace(out)
}
