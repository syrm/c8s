# REVUE DE CODE ACERBE - c8s
## Rapport d'audit de qualité et de sécurité

**Date**: 2026-01-08
**Auditeur**: Claude Code
**Portée**: Architecture complète (16 fichiers Go, ~2500 lignes)

---

## RÉSUMÉ EXÉCUTIF

Ce code souffre de **problèmes critiques** qui doivent être corrigés impérativement avant toute mise en production. L'application présente des risques de:
- **Fuites de mémoire** (goroutines orphelines, timers non nettoyés)
- **Data races** (accès concurrents non protégés)
- **Deadlocks** (mauvaise gestion des channels)
- **Corruption de données** (copie par valeur au lieu de pointeurs)

**Note globale**: 3/10 - Le code fonctionne mais est fragile et contient des bugs latents.

---

## 1. PROBLÈMES CRITIQUES (DOIVENT ÊTRE CORRIGÉS)

### 1.1 DATA RACE ET LOGIQUE ERRONÉE - `updateLocalCache`

**Fichier**: `tui/actions.go:79-87`

```go
func (t *Tui) updateLocalCache(containerID dto.ContainerID, action string) {
    t.tableContainerDataLock.Lock()
    if c, ok := t.tableContainerData[containerID]; ok {
        c.PendingAction = action  // MODIFIE LA COPIE LOCALE!
        t.tableContainerData[containerID] = c  // REMET LA COPIE
    }
    t.tableContainerDataLock.Unlock()
}
```

**Problème**: Ce code ne fait RIEN d'utile et a une data race:
1. `c` est une **copie par valeur** de `dto.Container` dans la map
2. Modifier `c.PendingAction` modifie la copie locale, PAS la valeur dans la map
3. Remettre `c` dans la map crée une nouvelle copie
4. Entre la lecture et l'écriture, une autre goroutine peut modifier la map

**Solution**: Utiliser un pointeur dans la map OU faire une modification atomique.

### 1.2 FUIte DE GOROUTINES - `getContainerStatsRealtime`

**Fichier**: `docker/docker.go:555` et `docker/docker.go:720-723`

```go
// Ligne 555 - Première goroutine
go d.getContainerStatsRealtime(ctx, c)

// Ligne 720-723 - NOUVELLE goroutine sur ActionStart!
if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
    go d.getContainerStatsRealtime(ctx, c)  // ANCIENNE GOROUTINE TOUJOURS ACTIVE!
}
```

**Problème**: Chaque fois qu'un conteneur redémarre, une NOUVELLE goroutine est créée **sans arrêter l'ancienne**. Après 100 redémarrages, vous avez 100 goroutines qui font la même chose.

**Impact**: Fuite de mémoire et CPU, consommation de ressources Docker API exponentielle.

**Solution**: Track les goroutines actives et les cancel avant d'en lancer une nouvelle.

### 1.3 TIME.AFTER() DANS BOUCLE INFINIE - Fuite de timers

**Fichiers**: `docker/docker.go` (lignes 149, 161, 207, 248, 251, 285, 311, 319, etc.)

```go
select {
case d.containersCommand <- ContainersCommand{...}:
case <-time.After(dto.ChannelTimeout):  // CRÉE UN NOUVEAU TIMER À CHAQUE FOIS!
    d.logger.Warn("timeout...")
    return
case <-ctx.Done():
    return
}
```

**Problème**: `time.After()` crée un nouveau timer à chaque appel. Dans une boucle, si le timeout n'est jamais atteint, les timers s'accumulent en mémoire.

**Impact**: Fuite de mémoire progressive, surtout sous charge.

**Solution**: Utiliser `time.NewTimer()` avec reset OU un ticker global.

### 1.4 RACE CONDITION SUR `statusTimer`

**Fichier**: `tui/tui.go:498-503`

```go
if t.statusTimer != nil {
    t.statusTimer.Stop()  // PAS DE LOCK!
}
t.statusTimer = time.AfterFunc(statusMessageDuration, func() {
    // ...
})
```

**Problème**: Pas de mutex protégeant l'accès à `statusTimer`. Deux goroutines peuvent accéder simultanément à ce champ.

### 1.5 DEADLOCK POTENTIEL - Channels non bufferisés

**Fichier**: `dto/request.go:10-38`

```go
type RequestProjectList struct {
    Response chan []Project  // PAS DE BUFFER!
}
```

**Problème**: Si le sender envoie avant que le receiver ne soit prêt, ou si le receiver attend après l'envoi, **deadlock garanti**.

**Exemple dans `docker.go:151`**:
```go
r.Response <- dto.Container{}  // PEUT BLOQUER ICI
return
```

Si la TUI ne consomme pas le channel immédiatement, la goroutine Docker se bloque.

### 1.6 DANGER: LogCancel dans le DTO

**Fichier**: `dto/container.go:29`

```go
type Container struct {
    // ...
    LogCancel context.CancelFunc  // DANGER!
}
```

**Problème**: Stocker une `CancelFunc` dans un DTO est:
1. **Mauvaise architecture** - Le DTO ne doit pas contenir de logique
2. **Non thread-safe** - CancelFunc n'est pas safe en accès concurrent
3. **Data race potentielle** - Si deux goroutines appellent LogCancel()

---

## 2. PROBLÈMES MAJEURS (DEVRAIENT ÊTRE CORRIGÉS)

### 2.1 ARCHITECTURE DES MUTEX - Sur-conception dangereuse

**Fichier**: `tui/tui.go:20-83` - 20+ mutex pour des champs individuels

```go
type Tui struct {
    logPaused                  bool
    logPausedLock              sync.RWMutex  // 1
    logShowTimestamp           bool
    logShowTimestampLock       sync.RWMutex  // 2
    logFilter                  string
    logFilterLock              sync.RWMutex  // 3
    currentView                currentView
    currentViewLock            sync.RWMutex  // 4
    containerRefreshPaused     bool
    containerRefreshPausedLock sync.RWMutex  // 5
    projectRefreshPaused       bool
    projectRefreshPausedLock   sync.RWMutex  // 6
    // ... et 14 autres mutex!
}
```

**Problème**: Cette architecture est:
1. **Verbeuse** - Plus de 150 lignes de getters/setters inutiles
2. **Lente** - Chaque accès nécessite un lock/unlock
3. **Complexe** - Facile de faire une erreur (oublier un lock)
4. **Incohérente** - Certains champs sont ensemble (currentContainerLock) mais n'ont rien à voir

**Solution**: Utiliser une struct `tuiState` protégée par un seul mutex, ou utiliser `atomic` pour les booléens.

### 2.2 GOROUTINES NON TRACKÉES - `collectContainerLogs`

**Fichier**: `docker/docker.go:368-464`

```go
func (d *Docker) collectContainerLogs(ctx context.Context, c *Container) {
    // ...
    lines := make(chan string, logLineBufferSize)

    // Goroutine 1
    go func() {
        defer pw.Close()
        _, err := stdcopy.StdCopy(pw, pw, out)
    }()

    // Goroutine 2
    go func() {
        defer close(lines)
        reader := bufio.NewReader(pr)
        for {
            line, errReader := reader.ReadString('\n')
            // ...
        }
    }()
    // ...
}
```

**Problème**: Si le contexte est annulé, ces goroutines ne sont pas garanties de s'arrêter proprement. Elles peuvent devenir orphelines.

**Impact**: Fuite de file descriptors, mémoire, et goroutines.

### 2.3 COPIE INUTILE DE DONNÉES - `handleRequestContainerLog`

**Fichier**: `docker/docker.go:191-203`

```go
Logs: make([]string, len(container.Logs)),  // ALLOCATION
// ...
copy(dtoContainer.Logs, container.Logs)     // COPIE
```

**Problème**: Pourquoi copier les logs? Si la source est modifiée pendant la copie, on a une data race. Et ça consomme de la mémoire pour rien.

### 2.4 PANIQUE NON GÉRÉE - `handleContainersCommand`

**Fichier**: `docker/docker.go:623-648`

```go
func (d *Docker) handleContainersCommand(ctx context.Context) (err error) {
    defer func() {
        if r := recover(); r != nil {
            // LOG L'ERREUR MAIS NE LA PROPAGE PAS CORRECTEMENT
            err = fmt.Errorf("panic: %v", r)
        }
    }()
    // ...
}
```

**Problème**: Si une panique survient, l'erreur est retournée mais la goroutine peut continuer dans un état inconsistent.

### 2.5 MAUVAISE GESTION DES ERREURS

**Fichiers**: Multiples

```go
// docker/docker.go:559
dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
if err != nil {
    d.logger.ErrorContext(ctx, "container stats failed", ...)
    // ON CONTINUE QUAND MÊME!
}
```

**Problème**: Les erreurs sont logguées mais souvent ignorées. L'application continue dans un état inconsistent.

---

## 3. PROBLÈMES MOYENS

### 3.1 ALLOCATIONS EXCESSIVES

**Fichier**: `tui/tui.go:286-347` - `drawProjects()`

```go
func (t *Tui) drawProjects() {
    t.tableProjectDataLock.RLock()
    projects := make([]dto.Project, 0, len(t.tableProjectData))  // ALLOCATION
    for _, p := range t.tableProjectData {
        projects = append(projects, p)  // COPIE
    }
    t.tableProjectDataLock.RUnlock()
    // ...
}
```

**Problème**: À chaque refresh (toutes les 2 secondes!), on alloue et copie toutes les données.

### 3.2 LOGique DE TRI INEFFICACE

**Fichier**: `tui/sorting.go:66-104` - `compareProjects()`

```go
func compareProjects(a, b dto.Project, sortColumn projectSortColumn, ascending bool) int {
    var cmp int
    switch sortColumn {
    case projectSortCPU:
        if a.CPUPercentage < b.CPUPercentage {
            cmp = -1
        } else if a.CPUPercentage > b.CPUPercentage {
            cmp = 1
        }
    // ...
}
```

**Problème**: Pourquoi ne pas utiliser `cmp.Compare()` introduit dans Go 1.21? C'est plus propre et plus performant.

### 3.3 CODE RÉPÉTITIF - Getters/Setters

**Fichier**: `tui/setup.go:403-670`

267 lignes de getters/setters qui pourraient être générés par `go generate` ou éliminés avec une meilleure architecture.

```go
func (t *Tui) setLogPaused(paused bool) {
    t.logPausedLock.Lock()
    defer t.logPausedLock.Unlock()
    t.logPaused = paused
}

func (t *Tui) getLogPaused() bool {
    t.logPausedLock.RLock()
    defer t.logPausedLock.RUnlock()
    return t.logPaused
}
// ... ×60 autres fonctions similaires
```

### 3.4 CONSTANTES NON CENTRALISÉES

**Fichiers**: `tui/constants.go` et `docker/docker.go`

```go
// tui/constants.go
const channelTimeout = dto.ChannelTimeout  // RENOMMAGE

// docker/docker.go
const (
    dockerAPITimeout = 30 * time.Second
    logHistoryDuration = 1 * time.Hour
    // ...
)
```

**Problème**: Certaines constantes sont dans `dto`, d'autres dans `tui`, d'autres dans `docker`. Pas de cohérence.

### 3.5 FONCTION `stripWarningPrefix` BUGGÉE

**Fichier**: `tui/sorting.go:12-18`

```go
func stripWarningPrefix(text string) string {
    if strings.HasPrefix(text, warningPrefix) {
        return text[len(warningPrefix):]  // BUG SI PREFIXE PRÉSENT 2×
    }
    return text
}
```

**Problème**: Si le préfixe est présent deux fois (ce qui ne devrait pas arriver mais bon), seule la première occurrence est retirée. Et si le texte contient le préfixe au milieu, il est aussi retiré.

---

## 4. PROBLÈMES MINEURS

### 4.1 COMMENTAIRES REDONDANTS

```go
// Container status constants.
const (
    StatusRunning = "running"   // Container status constants.
    StatusExited  = "exited"    // Container status constants.
)
```

### 4.2 NOMS DE VARIABLES CONFUS

```go
var ctxCancel context.CancelFunc  // Pourquoi pas just "cancel"?
```

### 4.3 FONCTIONS TROP LONGUES

- `docker.go:handleEvents()` - 100+ lignes
- `tui.go:refreshContainerLog()` - Fonction complexe avec callbacks dans callbacks

### 4.4 IMPORTS NON UTILISÉS OU MANQUANTS

Vérifier avec `goimports` pour nettoyer.

---

## 5. PROBLÈMES D'ARCHITECTURE

### 5.1 COUPLAGE FORT ENTRE TUI ET DOCKER

La TUI connaît trop de détails sur la structure interne de Docker. Le pattern DTO est bien mais mal appliqué (cf. `LogCancel`).

### 5.2 PAS D'INTERFACE

Le code utilise des types concrets partout. Pas d'interface pour:
- Le client Docker (pour les tests)
- Le logger (pour les tests)

### 5.3 ERREURS NON PROPAGÉES

Les erreurs sont souvent "avalées" (logguées mais non retournées). Cela rend le débogage difficile.

---

## 6. RECOMMANDATIONS PRIORITAIRES

### IMMÉDIAT (Avant toute production)

1. **Corriger la fuite de goroutines** dans `getContainerStatsRealtime`
2. **Remplacer tous les `time.After()` en boucle** par des `time.NewTimer()` recyclés
3. **Corriger la data race** dans `updateLocalCache`
4. **Ajouter des buffers** aux channels de réponse
5. **Retirer `LogCancel` du DTO**

### COURT TERME

1. **Refactoriser l'architecture des mutex** - Réduire à 3-4 mutex maximum
2. **Ajouter des tests** - Actuellement: 0 tests
3. **Utiliser `errgroup`** pour tracker les goroutines
4. **Propager les erreurs correctement**

### MOYEN TERME

1. **Ajouter des interfaces** pour la testabilité
2. **Réduire les allocations** dans les boucles de refresh
3. **Utiliser `sync.Pool`** pour les objets fréquemment alloués
4. **Ajouter du monitoring** (métriques, health checks)

---

## 7. ANALYSE PAR FICHIER

| Fichier | Lignes | Critiques | Majeurs | Mineurs |
|---------|--------|-----------|---------|---------|
| `main.go` | 64 | 0 | 1 | 0 |
| `dto/*.go` | 72 | 3 | 0 | 1 |
| `docker/docker.go` | 752 | 8 | 4 | 2 |
| `docker/container.go` | 233 | 1 | 2 | 0 |
| `tui/tui.go` | 970 | 6 | 5 | 3 |
| `tui/setup.go` | 671 | 0 | 2 | 1 |
| `tui/actions.go` | 195 | 2 | 0 | 0 |
| `tui/sorting.go` | 141 | 0 | 0 | 2 |
| `tui/header.go` | 121 | 0 | 0 | 0 |
| `tui/log_formatter.go` | 214 | 0 | 0 | 1 |
| **TOTAL** | **3433** | **20** | **16** | **10** |

---

## 8. CONCLUSION

Ce code a une **architecture de base saine** (séparation des couches, pattern request/response), mais souffre de **problèmes d'implémentation sérieux** qui pourraient causer des pannes en production.

Les problèmes de concurrence et de fuite de ressources sont particulièrement préoccupants pour une application de monitoring longue durée.

**Note finale**: Le code fait ce qu'il doit faire, mais n'est PAS production-ready sans corrections majeures.

---

**Fin du rapport**
