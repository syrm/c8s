# Revue de Code Acerbe - c8s (v11) - Best Practices Go

**Date:** 2026-01-07
**Fichiers analysés:** 14 fichiers Go
**Note globale:** 8.5/10

---

## Synthèse

Cette revue se concentre exclusivement sur les **best practices Go** qui avaient été négligées dans les revues précédentes. Bien que l'architecture et la gestion de la concurrence soient exemplaires, plusieurs conventions Go standard ne sont pas respectées.

---

## Problèmes Majeurs (2)

### 1. Erreurs non wrappées avec contexte

**Fichiers:** `docker/docker.go:48, 467`

```go
// docker.go:48
if err != nil {
    logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
    return nil, err  // ❌ Erreur non wrappée
}

// docker.go:467
if err != nil {
    return err  // ❌ Erreur non wrappée
}
```

**Best practice Go:** Toujours wrapper les erreurs avec `fmt.Errorf("context: %w", err)` pour préserver la stack trace et ajouter du contexte.

**Correction:**
```go
// docker.go:48
return nil, fmt.Errorf("creating docker client: %w", err)

// docker.go:467
return fmt.Errorf("listing containers: %w", err)
```

---

### 2. Comparaison d'erreurs avec `==` au lieu de `errors.Is`

**Fichiers:** `docker/docker.go:402, 414, 583`

```go
// docker.go:402
if err != nil && err != io.EOF {  // ❌

// docker.go:414
if errReader != io.EOF {  // ❌

// docker.go:583
if errDecode != io.EOF && !errors.Is(errDecode, context.DeadlineExceeded) {
    // Incohérent: utilise errors.Is pour un cas mais pas l'autre
}
```

**Best practice Go:** Utiliser `errors.Is()` pour comparer les erreurs, car les erreurs peuvent être wrappées.

**Correction:**
```go
if err != nil && !errors.Is(err, io.EOF) {
```

---

## Problèmes Mineurs (8)

### 3. Types et fonctions exportés sans documentation

**Fichiers:** `docker/docker.go`, `docker/container.go`, `dto/*.go`

```go
// docker/docker.go:26 - ❌ Pas de doc
type ContainersCommand struct {

// docker/docker.go:31 - ❌ Pas de doc
type Docker struct {

// docker/docker.go:40 - ❌ Pas de doc
func NewDocker(

// docker/container.go:12 - ❌ Pas de doc
type ContainerID string

// docker/container.go:14 - ❌ Pas de doc
type Container struct {

// dto/container.go:5 - ❌ Pas de doc
type ContainerID string

// dto/container.go:7 - ❌ Pas de doc
type Container struct {
```

**Best practice Go:** Tout type ou fonction exporté doit avoir un commentaire de documentation commençant par le nom de l'élément.

**Correction:**
```go
// ContainersCommand represents a command to be executed on the containers map.
type ContainersCommand struct {

// Docker manages the connection to the Docker daemon and container monitoring.
type Docker struct {

// NewDocker creates a new Docker client and initializes monitoring.
func NewDocker(
```

---

### 4. Magic numbers sans constantes nommées

**Fichiers:** `docker/docker.go`, `tui/tui.go`, `tui/actions.go`

```go
// docker.go:54
containers: make(map[ContainerID]*Container, 256),  // ❌ Magic number

// docker.go:377
since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)  // ❌ Magic number

// docker.go:396
lines := make(chan string, 100)  // ❌ Magic number

// tui.go:312-313
if project.CPUPercentage > 80 || project.MemoryPercentage > 80 {  // ❌ Magic number

// tui/actions.go:99
cmd := exec.Command("docker", "exec", "-it", string(container.ID), "/bin/sh")  // ❌ Magic string
```

**Correction:**
```go
const (
    initialContainerMapSize = 256
    logHistoryDuration      = 1 * time.Hour
    logLineBufferSize       = 100
    resourceWarningThreshold = 80.0
    defaultShell            = "/bin/sh"
)
```

---

### 5. Paramètre non utilisé

**Fichier:** `tui/tui.go:255`

```go
func (t *Tui) RenderContainerHeader(project string) {  // ❌ project non utilisé
    sortCol, sortAsc := t.getContainerSort()
    // ... project n'est jamais utilisé dans la fonction
}
```

**Correction:** Supprimer le paramètre ou l'utiliser.

---

### 6. Status strings hardcodés (pas de constantes)

**Fichiers:** `docker/container.go`, `tui/tui.go`, `tui/actions.go`

```go
// Partout dans le code:
container.Status == "running"
container.Status = "exited"
container.Status != "running"
```

**Best practice Go:** Définir des constantes pour les valeurs string répétées.

**Correction:**
```go
const (
    StatusRunning    = "running"
    StatusExited     = "exited"
    StatusPaused     = "paused"
    StatusRestarting = "restarting"
    StatusCreated    = "created"
    StatusRemoving   = "removing"
)
```

---

### 7. Type switch sans case default

**Fichier:** `docker/docker.go:112-125`

```go
switch r := req.(type) {
case *model.RequestContainerLog:
    d.handleRequestContainerLog(ctx, r)
case *model.RequestProject:
    d.handleRequestContainerProject(ctx, r)
case *model.RequestSetPendingAction:
    d.handleRequestSetPendingAction(ctx, r)
case *model.RequestProjectList:
    d.handleRequestProjectList(ctx, r)
// ❌ Pas de default case
}
```

**Correction:**
```go
default:
    d.logger.Warn("unknown request type", slog.String("type", fmt.Sprintf("%T", req)))
```

---

### 8. Context shadowing

**Fichier:** `docker/container.go:51`

```go
func NewContainer(
    ctx context.Context,  // ctx original
    // ...
) *Container {
    ctx, cancel := context.WithCancel(ctx)  // ❌ ctx shadowed
```

**Best practice Go:** Éviter de shadower les paramètres pour clarté.

**Correction:**
```go
childCtx, cancel := context.WithCancel(ctx)
// ...
go c.handleCommands(childCtx)
```

---

### 9. ContainerID dupliqué dans deux packages

**Fichiers:** `docker/container.go:12`, `dto/container.go:5`

```go
// docker/container.go
type ContainerID string

// dto/container.go
type ContainerID string
```

**Problème:** Le même type est défini dans deux packages différents, nécessitant des conversions explicites.

**Correction:** Utiliser uniquement `model.ContainerID` partout.

---

### 10. Fonctions trop longues

**Fichiers:** `tui/tui.go`, `docker/docker.go`

| Fonction | Lignes | Recommandé |
|----------|--------|------------|
| `NewTui` | ~150 | < 50 |
| `handleRequestContainerLog` | ~90 | < 50 |
| `drawContainers` | ~110 | < 50 |
| `collectContainerLogs` | ~95 | < 50 |

**Best practice Go:** Les fonctions devraient être courtes et faire une seule chose. Extraire des sous-fonctions.

---

## Suggestions d'Amélioration (3)

### 11. Utiliser des sous-structures pour grouper les champs liés

**Fichier:** `tui/tui.go:20-81`

La struct `Tui` a 30+ champs. Considérer le groupement :

```go
type Tui struct {
    app    *tview.Application
    pages  *tview.Pages
    logger *slog.Logger

    projects   projectState
    containers containerState
    logs       logState
    // ...
}

type projectState struct {
    table       *tview.Table
    data        map[model.ProjectID]model.Project
    dataLock    sync.RWMutex
    searchInput *tview.InputField
    searchQuery string
    // ...
}
```

---

### 12. Interface segregation pour Docker

**Fichier:** `docker/docker.go`

La struct Docker fait beaucoup de choses. Considérer des interfaces plus petites :

```go
type ContainerLister interface {
    ListContainers(ctx context.Context) ([]Container, error)
}

type ContainerMonitor interface {
    MonitorStats(ctx context.Context, containerID string) (<-chan Stats, error)
}
```

---

### 13. Utiliser des options fonctionnelles pour NewDocker/NewTui

**Fichiers:** `docker/docker.go:40`, `tui/tui.go:83`

```go
// Au lieu de:
func NewDocker(ctx context.Context, requestData <-chan model.RequestData, logger *slog.Logger) (*Docker, error)

// Considérer:
type DockerOption func(*Docker)

func WithLogger(logger *slog.Logger) DockerOption {
    return func(d *Docker) { d.logger = logger }
}

func NewDocker(ctx context.Context, opts ...DockerOption) (*Docker, error)
```

---

## Statistiques

| Catégorie | Nombre |
|-----------|--------|
| Problèmes majeurs | 2 |
| Problèmes mineurs | 8 |
| Suggestions | 3 |

---

## Checklist Best Practices Go

| Règle | Status |
|-------|--------|
| Error wrapping avec `%w` | ❌ Manquant |
| `errors.Is()` pour comparaisons | ❌ Incohérent |
| Documentation des exports | ❌ Manquante |
| Constantes pour magic numbers | ❌ Manquantes |
| Constantes pour status strings | ❌ Manquantes |
| Paramètres utilisés | ❌ 1 non utilisé |
| Default case dans type switch | ❌ Manquant |
| Éviter le shadowing | ❌ 1 cas |
| Fonctions < 50 lignes | ❌ 4+ fonctions longues |
| DRY (types non dupliqués) | ❌ ContainerID dupliqué |
| Receiver names cohérents | ✅ OK |
| Context en premier paramètre | ✅ OK |
| Error en dernier retour | ✅ OK |
| Defer après error check | ✅ OK |
| Range variable capture | ✅ OK |

---

## Comparaison avec les revues précédentes

| Aspect | Revues v3-v10 | Revue v11 |
|--------|--------------|-----------|
| Concurrence | ✅ Exemplaire | (non réévalué) |
| Data races | ✅ Aucun | (non réévalué) |
| Memory leaks | ✅ Aucun | (non réévalué) |
| IO blocking | ✅ Protégé | (non réévalué) |
| **Best practices Go** | Non évalué | ❌ 10 problèmes |

---

## Conclusion

Le code est fonctionnellement excellent avec une architecture solide et une gestion de concurrence irréprochable. Cependant, plusieurs best practices Go standard ne sont pas respectées :

1. **Error handling** : Les erreurs ne sont pas wrappées et `errors.Is` n'est pas utilisé systématiquement
2. **Documentation** : Les types et fonctions exportés manquent de documentation
3. **Constantes** : Magic numbers et strings répétés sans constantes
4. **Organisation** : Fonctions trop longues, types dupliqués

**Priorité de correction:**
1. **Haute** : Wrapper les erreurs avec `fmt.Errorf("context: %w", err)`
2. **Haute** : Utiliser `errors.Is()` pour les comparaisons d'erreurs
3. **Moyenne** : Ajouter la documentation des exports
4. **Moyenne** : Extraire les magic numbers en constantes
5. **Basse** : Refactorer les fonctions longues

**Note ajustée:** 8.5/10 (pénalité pour les manquements aux best practices Go)
