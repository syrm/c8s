# Rapport de Revue de Code (c8s)

**Date :** 13 Janvier 2026
**Projet :** c8s
**Revue par :** Gemini (Expert Go/Architecture)

## Synthèse

Le code fonctionne peut-être, mais il souffre d'un grave problème d'identité : c'est du Java écrit avec la syntaxe de Go. L'architecture est inutilement complexe sur des points triviaux (wrappers de synchronisation génériques) et dangereusement naïve sur des points critiques (gestion des sous-processus, I/O bloquants).

La lisibilité est entravée par une "God Struct" (`Tui`) et une sur-ingénierie des structures de données (`dto`, `SyncMap`).

## 1. Architecture et Idiomes Go (Le "Java-isme")

### ❌ Le Package `dto`
**Problème :** L'existence même d'un package nommé `dto` est un anti-pattern en Go. Go n'est pas Java. On ne sépare pas artificiellement les données de leur comportement via des "Data Transfer Objects" anémiques.
**Correction :** Définissez les types là où ils font sens (dans le domaine métier ou près de leur utilisation). Si `Project` et `Container` sont le cœur du modèle, ils devraient être dans un package `model` ou à la racine, avec des méthodes associées, pas des structs vides baladées partout.

### ❌ Wrappers `SyncMap` / `SyncSlice` (`tui/sync.go`)
**Problème :** C'est de la sur-ingénierie classique. Vous avez réinventé `sync.Map` (en version typée mais moins performante pour certains cas) et créé des wrappers avec des Getters/Setters (`Get()`, `Set()`). C'est verbeux, lent (copie à chaque lecture pour `SyncSlice`) et non idiomatique.
**Correction :** Utilisez simplement un `sync.RWMutex` directement dans la struct qui possède les données.
```go
type ProjectView struct {
    mu    sync.RWMutex // Protège les champs suivants
    Data  map[string]Project
    // ...
}
```
C'est plus clair, plus performant et tout développeur Go comprendra immédiatement ce qui est protégé.

### ❌ Pattern Functor pour `ContainerCommand` (`docker/container.go`)
**Problème :** Envoyer des fermetures (closures) via un channel pour modifier l'état interne d'une struct est un pattern "Actor" intéressant, mais ici il est mélangé avec une gestion de mutex (`logsMu`). C'est confus. On a deux modèles de concurrence qui se battent : *Share memory by communicating* (le channel de commandes) vs *Communicate by sharing memory* (le mutex des logs).
**Correction :** Choisissez une stratégie. Vu la simplicité des mises à jour d'état, un simple `sync.Mutex` sur la struct `Container` serait probablement beaucoup plus simple et moins sujet aux deadlocks subtils qu'un channel de commandes bufférisé.

## 2. Problèmes de Concurrence et Performance

### ⚠️ I/O Bloquant au Démarrage (`docker/docker.go`)
**Problème :** `findPodmanSocket` utilise `exec.Command(...).Output()` de manière synchrone et sans timeout. Si la commande `podman` pend (ce qui arrive souvent avec des montages réseau ou des drivers FS corrompus), toute votre application freeze au démarrage sans log ni explication.
**Correction :** Utilisez `exec.CommandContext` avec un timeout strict (ex: 2 secondes).

### ⚠️ Risque de Freeze UI (`tui/tui.go`)
**Problème :** Les méthodes `drawProjects` et `drawContainers` effectuent des tris (`slices.SortStableFunc`) et des filtres directement dans la goroutine de rendu UI (`QueueUpdateDraw`). Si vous avez beaucoup de conteneurs, l'interface va lagger à chaque rafraîchissement.
**Correction :** Le tri et le filtrage sont des opérations "métier". Elles doivent être faites dans une goroutine de travail, et seule la liste finale prête à afficher doit être passée à l'UI.

### ⚠️ "God Struct" `Tui`
**Problème :** La struct `Tui` contient tout : état de l'UI, logique de navigation, communication réseau (`requestData`), gestion des modales, configuration. Le fichier `tui.go` est un fourre-tout.
**Correction :** Découpez ! `ProjectListController`, `ContainerListController`, `LogController`. Chaque composant devrait gérer sa propre logique et son propre état. `Tui` ne devrait être qu'un chef d'orchestre.

## 3. Qualité du Code et Bonnes Pratiques

### ❌ Gestion des Logs (`docker/logs.go`)
**Problème :** Vous copiez manuellement les logs via `stdcopy` puis un pipe, puis un scanner. C'est lourd.
**Correction :** Docker fournit déjà des options pour multiplexer/démultiplexer. De plus, `processLogStream` lance 3 goroutines (`g.Go`) pour gérer un seul flux. C'est beaucoup de complexité pour lire des lignes.

### ❌ Magic Numbers (`tui/tui.go`)
**Problème :** Le code est truffé de valeurs magiques pour la largeur des colonnes (`SetExpansion(3)`, `SetMaxWidth(7)`). Si on change la police ou la résolution, tout casse.
**Correction :** Définissez des constantes nommées ou calculez ces valeurs dynamiquement en fonction du contenu.

### ❌ `Getters` Inutiles
**Problème :** `tui/tui.go` : `func (t *Tui) GetRequestData()`. En Go, on accède souvent directement aux champs des structs du même package, ou on expose le channel si c'est l'API publique. Faire un getter pour un channel interne est superflu.

### ❌ Complexité Cyclomatique
**Problème :** `drawContainers` est trop complexe. Elle gère le tri, le filtrage, la coloration du statut, le formatage des chaînes et l'ajout au tableau.
**Correction :** Extrayez la logique de présentation (ex: `getContainerColor(status string)`) et la logique de filtrage dans des fonctions pures testables unitairement.

## Conclusion

Le projet démontre une bonne volonté de gérer la concurrence proprement (utilisation de `context`, `errgroup`), mais il trébuche sur la complexité accidentelle introduite par une tentative de trop bien faire (génériques inutiles, patterns de synchronisation exotiques).

**Priorités immédiates :**
1.  Supprimer le package `dto` et ramener les types dans le domaine.
2.  Simplifier la synchronisation (remplacer `SyncMap`/`SyncSlice` par des mutex simples).
3.  Sécuriser les appels système (`exec`) avec des timeouts.
4.  Refactoriser `Tui` pour sortir la logique métier de la couche de présentation.
