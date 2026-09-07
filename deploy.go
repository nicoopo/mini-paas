package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runCmd exécute une commande, capture stdout+stderr, et renvoie le tout même en cas d'erreur.
// Réservé aux commandes rapides et synchrones (stop, logs, status, docker rm).
func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// runCmdStream exécute une commande et écrit sa sortie (stdout+stderr mêlés) au fil de l'eau dans w,
// pour un suivi en direct (utilisé par le pipeline de déploiement, diffusé ensuite en SSE).
func runCmdStream(dir string, w io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func repoDir(p Project) string {
	return filepath.Join("data", "repos", p.ID)
}

// cloneOrPull clone le repo s'il n'existe pas encore, sinon fetch+pull, en streamant la sortie dans w.
// Si p.Branch est vide, on reste sur la branche par défaut du remote.
func cloneOrPull(p Project, w io.Writer) error {
	dir := repoDir(p)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return pullBranch(dir, p.Branch, w)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	args := []string{"clone"}
	if p.Branch != "" {
		args = append(args, "-b", p.Branch)
	}
	args = append(args, p.RepoURL, dir)
	return runCmdStream(".", w, "git", args...)
}

// pullBranch met à jour un clone existant : fetch, puis bascule sur la branche demandée
// si elle diffère de celle déjà checkoutée, puis pull.
// "git checkout -B <branch> origin/<branch>" (re)crée la branche locale pile sur le remote :
// pas de gestion de divergence/merge, cohérent avec l'esprit outil de déploiement (pas de dev ici).
func pullBranch(dir, branch string, w io.Writer) error {
	if err := runCmdStream(dir, w, "git", "fetch", "origin"); err != nil {
		return err
	}
	if branch != "" {
		if err := runCmdStream(dir, w, "git", "checkout", "-B", branch, "origin/"+branch); err != nil {
			return err
		}
	}
	return runCmdStream(dir, w, "git", "pull")
}

// dockerBuild suppose un Dockerfile à la racine du repo, comme n'importe quel PaaS basé sur Docker.
func dockerBuild(p Project, w io.Writer) error {
	return runCmdStream(repoDir(p), w, "docker", "build", "-t", p.ContainerName(), ".")
}

// dockerRun retire l'ancien conteneur s'il existe puis relance l'image fraîchement buildée.
func dockerRun(p Project, w io.Writer) error {
	name := p.ContainerName()
	// Erreur ignorée et affichée seulement si non vide : "No such container" au tout premier déploiement.
	if removeOut, _ := runCmd(".", "docker", "rm", "-f", name); removeOut != "" {
		fmt.Fprintln(w, removeOut)
	}

	containerPort := p.ContainerPort
	if containerPort == "" {
		containerPort = "80"
	}
	portMap := fmt.Sprintf("%s:%s", p.HostPort, containerPort)

	args := []string{"run", "-d", "--name", name, "-p", portMap}
	for _, kv := range p.EnvVars {
		args = append(args, "-e", kv)
	}
	args = append(args, name)

	return runCmdStream(".", w, "docker", args...)
}

// Deploy enchaîne clone/pull -> build -> run, en streamant chaque étape dans w au fur et à mesure,
// et s'arrête à la première étape qui échoue.
func Deploy(p Project, w io.Writer) error {
	fmt.Fprintln(w, "$ git clone/pull")
	if err := cloneOrPull(p, w); err != nil {
		return fmt.Errorf("git a échoué: %w", err)
	}

	fmt.Fprintln(w, "\n$ docker build")
	if err := dockerBuild(p, w); err != nil {
		return fmt.Errorf("docker build a échoué: %w", err)
	}

	fmt.Fprintln(w, "\n$ docker run")
	if err := dockerRun(p, w); err != nil {
		return fmt.Errorf("docker run a échoué: %w", err)
	}

	return nil
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
