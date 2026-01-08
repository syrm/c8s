# Revue de Code Acerbe - c8s (v8)

**Date:** 2026-01-07
**Fichiers analysés:** 14 fichiers Go
**Note globale:** 9.5/10

---

## Synthèse

Le code a atteint un niveau de qualité exceptionnelle. L'architecture est exemplaire et la gestion de la concurrence est irréprochable. Cette revue identifie un bug fonctionnel potentiel lié au matching de noms similaires, ainsi que quelques éléments de code mort ou dupliqué.

---

## Problèmes Critiques (0)

Aucun problème critique de concurrence ou de sécurité identifié.

---

## Problèmes Majeurs (1)

### 1. Bug : Matching ambigu de projets/containers avec noms similaires

**Fichiers:** `tui/setup.go:110-122`, `tui/setup.go:228-240`, `tui/actions.go:35-36`

```go
// setup.go:110-122 - enterContainerView()
for _, project := range t.tableProjectData {
    cellText := cell.Text
    if fuzzyMatch(cellText, project.Name) {  // BUG: peut matcher plusieurs projets
        t.setCurrentProjectID(string(project.ID))
        break
    }
}
```

**Problème:** Si deux projets ont des noms où l'un est un préfixe de l'autre (ex: "api" et "api-gateway"), `fuzzyMatch("api-gateway", "api")` retourne `true` car tous les caractères de "api" apparaissent dans "api-gateway".

Comme l'itération sur une map Go est non-déterministe, sélectionner "api-gateway" pourrait incorrectement matcher le projet "api" si celui-ci est itéré en premier.

**Scénarios affectés:**
- `enterContainerView()` : Sélection d'un projet
- `enterLogView()` : Sélection d'un container
- `getSelectedContainer()` : Actions sur un container

**Impact:** L'utilisateur pourrait naviguer vers le mauvais projet ou exécuter une action sur le mauvais container si des noms similaires existent.

**Correction:** Utiliser une correspondance exacte ou vérifier l'égalité stricte après le fuzzyMatch :
```go
for _, project := range t.tableProjectData {
    cellText := cell.Text
    // Vérifier que le nom du projet correspond exactement au texte
    // (en ignorant les marqueurs de formatage comme [yellow]⚠[-])
    cleanCellText := stripFormatting(cellText)
    if cleanCellText == project.Name {
        t.setCurrentProjectID(string(project.ID))
        break
    }
}
```

Ou utiliser le fuzzyMatch dans le bon sens puis vérifier la longueur :
```go
if fuzzyMatch(cellText, project.Name) && len(project.Name) >= len(stripFormatting(cellText))-10 {
```

---

## Problèmes Mineurs (3)

### 2. Champ `logger` non utilisé dans Container (Code mort)

**Fichier:** `docker/container.go:28`

```go
type Container struct {
    // ...
    logger *slog.Logger  // Assigné ligne 80, jamais utilisé
}
```

**Problème:** Le champ `logger` est assigné dans `NewContainer()` mais n'est jamais utilisé dans aucune méthode de `Container`. Ce champ consomme de la mémoire pour chaque container sans apporter de valeur.

**Impact:** Consommation mémoire inutile (~8 bytes par container sur architecture 64-bit).

**Correction:** Supprimer le champ ou l'utiliser pour logger les opérations du container.

---

### 3. Constante `channelTimeout` dupliquée

**Fichiers:** `docker/docker.go:462`, `tui/constants.go:10`

```go
// docker/docker.go:462
const channelTimeout = 5 * time.Second

// tui/constants.go:10
const channelTimeout = 5 * time.Second
```

**Problème:** La même constante est définie dans deux packages différents. Si une valeur est modifiée sans l'autre, cela pourrait créer des comportements incohérents.

**Impact:** Risque de divergence lors de modifications futures.

**Correction:** Définir la constante dans un seul endroit (par exemple `dto/constants.go`) et l'importer.

---

### 4. Manipulation de chaîne fragile

**Fichier:** `tui/actions.go:163`

```go
t.showStatusMessage(fmt.Sprintf("Failed to %s container: %v", action[:len(action)-3], err))
```

**Problème:** Cette manipulation transforme "restarting" → "restart" et "starting" → "start" en retirant 3 caractères. Le code repose sur le fait que toutes les actions se terminent par "ing" et font au moins 4 caractères.

**Impact:** Code fragile qui pourrait planter si une action différente est ajoutée.

**Correction:**
```go
verb := strings.TrimSuffix(action, "ing")
if verb == action {
    verb = action // Fallback si pas de suffix "ing"
}
t.showStatusMessage(fmt.Sprintf("Failed to %s container: %v", verb, err))
```

---

## Suggestions d'Amélioration (2)

### 5. Chemin du fichier de log hardcodé

**Fichier:** `main.go:24`

```go
file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
```

**Observation:** Le chemin "app.log" est en dur. En production, l'emplacement des logs devrait être configurable.

**Suggestion:** Accepter un flag ou une variable d'environnement pour le chemin du log.

---

### 6. Pas de vérification de version Docker minimale

**Fichier:** `docker/docker.go:45`

```go
cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
```

**Observation:** L'API version negotiation est utilisée mais il n'y a pas de vérification que la version du daemon Docker supporte toutes les fonctionnalités utilisées (stats streaming, events, etc.).

**Suggestion:** Ajouter une vérification de version minimale au démarrage :
```go
serverVersion, err := cli.ServerVersion(ctx)
if err != nil || serverVersion.APIVersion < "1.25" {
    return nil, fmt.Errorf("Docker API version 1.25+ required")
}
```

---

## Points Positifs

1. **Architecture exemplaire** : Séparation claire TUI/Docker avec communication par channels
2. **Aucun data race** : Pattern command channel correctement appliqué partout
3. **Timeouts systématiques** : Toutes les opérations channel ont un timeout
4. **Shutdown gracieux** : Pattern `done` channel + `Wait()` bien implémenté
5. **Copie des données** : Les slices sont correctement copiées pour éviter les races
6. **Protection des états partagés** : RWMutex utilisés de manière cohérente
7. **Gestion des erreurs** : Les erreurs Docker sont correctement gérées et loggées
8. **Recovery panic** : `handleContainersCommand` a un recover qui convertit en erreur
9. **Nettoyage des ressources** : `cleanup()` ferme proprement les timers et channels
10. **Code lisible** : Fonctions bien découpées avec des noms explicites
11. **Contexte propagé** : Toutes les fonctions de requête ont un paramètre ctx
12. **Logs appropriés** : Niveau DEBUG pour les opérations de routine

---

## Statistiques

| Catégorie | Nombre |
|-----------|--------|
| Problèmes critiques | 0 |
| Problèmes majeurs | 1 |
| Problèmes mineurs | 3 |
| Suggestions | 2 |

---

## Vérification de la Concurrence

### Analyse des Goroutines

| Goroutine | Protection | Status |
|-----------|------------|--------|
| `handleContainersCommand` | Channel serialization | ✅ OK |
| `handleEvents` | Channel + timeout | ✅ OK |
| `handleRequests` | Channel + timeout | ✅ OK |
| `collectContainers` | Channel + timeout | ✅ OK |
| `getContainerStatsRealtime` | Channel + timeout | ✅ OK |
| `collectContainerLogs` | Channel + timeout | ✅ OK |
| `Container.handleCommands` | Channel serialization | ✅ OK |
| `Tui.getData` | RWMutex + Channel | ✅ OK |
| Actions TUI (stop/restart/rm) | Goroutine + QueueUpdateDraw | ✅ OK |

### Analyse des Champs Partagés

| Champ | Protection | Status |
|-------|------------|--------|
| `containers` map | `containersCommand` channel | ✅ OK |
| `Container.*` fields | `Container.Command` channel | ✅ OK |
| `tableProjectData` | `tableProjectDataLock` | ✅ OK |
| `tableContainerData` | `tableContainerDataLock` | ✅ OK |
| `tableContainerLogData` | `tableContainerLogDataLock` | ✅ OK |
| `currentView` | `currentViewLock` | ✅ OK |
| `currentProjectID/ContainerID` | `currentContainerLock` | ✅ OK |
| `*SortColumn/*SortAsc` | `*SortLock` | ✅ OK |
| `logPaused/Filter/Timestamp` | Individual locks | ✅ OK |
| `containerDisappeared` | `containerDisappearedLock` | ✅ OK |
| `*RefreshPaused` | Individual locks | ✅ OK |
| `currentTableWidth` | `atomic.Int32` | ✅ OK |

---

## Analyse de la Gestion Mémoire

| Aspect | Implémentation | Status |
|--------|----------------|--------|
| Logs limités | `maxLogLines = 1000` | ✅ OK |
| Logs effacés à la sortie | `clearTableContainerLogData()` | ✅ OK |
| Logs effacés à la suppression | `c.Logs = nil` dans `Delete()` | ✅ OK |
| Containers nettoyés | `delete(docker.containers, c.ID)` | ✅ OK |
| Contextes annulés | `cancel()` appelé systématiquement | ✅ OK |
| Timers arrêtés | `Stop()` dans `cleanup()` | ✅ OK |
| Channels fermés | `close(t.requestData)` | ✅ OK |

---

## Conclusion

Le code est de **qualité production** avec un seul bug fonctionnel potentiel lié au matching de noms similaires. Ce bug ne cause pas de crash mais pourrait conduire à une mauvaise sélection dans des cas edge.

**Priorité de correction:**
1. **Haute** : Corriger le bug de matching ambigu (problème #1)
2. **Basse** : Supprimer le champ `logger` non utilisé dans Container
3. **Basse** : Centraliser la constante `channelTimeout`
4. **Basse** : Sécuriser la manipulation de chaîne dans actions.go

**Évolution de la note:**
- CODE_REVIEW_3: 7/10
- CODE_REVIEW_4: 8/10
- CODE_REVIEW_5: 8.5/10
- CODE_REVIEW_6: 9/10
- CODE_REVIEW_7: 9.5/10
- CODE_REVIEW_8: **9.5/10**

Le projet reste prêt pour la production. Le bug identifié n'affecte que les cas où des projets/containers ont des noms où l'un est préfixe de l'autre, ce qui est relativement rare en pratique.
