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

Dashboard sur `http://127.0.0.1:8090`. Ajoute un projet (nom, URL du repo, branche optionnelle, port
hôte, port conteneur — 80 par défaut), clique **Déployer**. La requête reste ouverte le temps du
clone+build+run et affiche la sortie complète en dessous.

Branche laissée vide → reste sur la branche par défaut du remote. Une fois fixée à la création d'un
projet, la changer implique de supprimer/recréer le projet (pas d'édition pour le MVP).

Les projets sont stockés dans `data/projects.json`, les clones dans `data/repos/<id>/` — tout est
dans le dossier courant, rien n'est écrit ailleurs sur la machine.

## Limites volontaires du MVP (commentaires `ponytail:` dans le code)

- Déploiement synchrone : la requête HTTP bloque jusqu'à la fin du build. Pour des builds longs,
  passer à un job en arrière-plan + Server-Sent Events pour streamer les logs en direct.
- Aucune authentification — pensé pour tourner en local sur ta machine, pas exposé sur Internet.
- Pas de gestion de variables d'environnement à injecter dans le conteneur déployé.

À ajouter si tu veux vraiment t'en servir au quotidien pour plusieurs projets ; pas avant.
