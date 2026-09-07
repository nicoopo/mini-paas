# mini-PaaS

Un binaire Go unique, zéro dépendance externe, qui déploie un projet depuis son dépôt Git dans un
conteneur Docker local, piloté par un petit dashboard web. Le déploiement fait : `git clone`/`pull`
→ `docker build` → `docker run`, en shellant directement les CLI `git` et `docker` déjà installées
sur ta machine (pas de SDK Docker, pas de go-git — juste `os/exec` et la stdlib).

## Pré-requis

- Le repo à déployer doit avoir un **Dockerfile à sa racine** — comme n'importe quel PaaS basé sur
  Docker (Heroku/Dokku utilisent des buildpacks, ici on part direct du Dockerfile).
- `git` et `docker` doivent être dans le `PATH` de la machine qui lance mini-PaaS.

## Lancer

```bash
go build -o mini-paas .
./mini-paas
```

Le dashboard est protégé par HTTP Basic Auth (un seul mot de passe partagé, login ignoré). Fixe-le
via `PAAS_PASSWORD` :

```bash
PAAS_PASSWORD=change-moi ./mini-paas
```

Si `PAAS_PASSWORD` n'est pas défini, un mot de passe est généré au démarrage et affiché dans les
logs — pratique pour tester, mais il change à chaque redémarrage.

Dashboard sur `http://127.0.0.1:8090`. Ajoute un projet (nom, URL du repo, branche optionnelle,
variables d'env optionnelles, port hôte, port conteneur — 80 par défaut), clique **Déployer**. Le
déploiement tourne en arrière-plan ; la sortie de `git`/`docker` s'affiche en direct via
Server-Sent Events au fur et à mesure, sans bloquer la requête. Un seul déploiement à la fois par
projet (le bouton renvoie une erreur 409 si tu redéploies pendant qu'un déploiement est déjà en cours).

Branche laissée vide → reste sur la branche par défaut du remote. Une fois fixée à la création d'un
projet, la changer implique de supprimer/recréer le projet (pas d'édition pour le MVP).

Variables d'environnement : une par ligne au format `KEY=VALUE` dans le champ dédié, injectées au
conteneur via `docker run -e`. Lignes vides et lignes commençant par `#` ignorées.

Les projets sont stockés dans `data/projects.json`, les clones dans `data/repos/<id>/` — tout est
dans le dossier courant, rien n'est écrit ailleurs sur la machine.

## Limites volontaires du MVP (commentaires `ponytail:` dans le code)

- Jobs de déploiement en mémoire, perdus au redémarrage du process — acceptable, un déploiement en
  cours ne survivrait de toute façon pas à un arrêt du binaire.
- Auth minimale (un seul mot de passe partagé, pas de compte multi-utilisateur) — suffisant pour un
  outil perso, pas pour une exposition publique sérieuse.

À ajouter si tu veux vraiment t'en servir au quotidien pour plusieurs projets ; pas avant.
