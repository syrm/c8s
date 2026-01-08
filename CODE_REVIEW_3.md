# Revue de Code Acerbe - c8s (Troisième Revue)

## Note Globale : 7/10

Le code a été significativement amélioré depuis les revues précédentes. La gestion des timeouts et des locks est bien meilleure. Cependant, plusieurs problèmes persistent et de nouveaux ont été introduits.

---

## PROBLEMES CRITIQUES

### 1. Race Conditions Persistantes dans TUI Layer

**`tui/tui.go:27,35`** - `projectSearchActive` et `containerSearchActive` sans lock
```go
type Tui struct {
    // ...
    projectSearchActive        bool      // Accede depuis input handlers et callbacks
    containerSearchActive      bool      // Accede depuis input handlers et callbacks
```

Ces booléens sont accédés depuis les callbacks de `SetInputCapture` (goroutine principale) et potentiellement depuis d'autres goroutines sans protection.

**`tui/tui.go:67-70`** - Variables de tri sans protection
```go
    projectSortColumn          projectSortColumn  // Pas de lock
    projectSortAsc             bool               // Pas de lock
    containerSortColumn        containerSortColumn
    containerSortAsc           bool
```

Ces variables sont modifiées dans `setProjectSort`/`setContainerSort` et lues dans `drawProjects`/`drawContainers` qui peuvent être appelées depuis `app.QueueUpdateDraw`.

**`tui/tui.go:71`** - `currentProjectName` sans lock dédié
```go
    currentProjectName         string  // Utilise mais pas protege par currentContainerLock
```

**`tui/setup.go:120`** - Accès direct non protégé
```go
t.setCurrentProjectID(string(project.ID))
t.currentProjectName = project.Name    // Acces direct sans lock!
```

---

### 2. Pas de Fermeture du Client Docker

**`main.go:42-46`** - Le client Docker n'est jamais fermé
```go
doc, err := docker.NewDocker(ctx, t.GetRequestData(), logger)
if err != nil {
    return fmt.Errorf("failed to initialize docker client: %w", err)
}
go doc.Run(ctx)
// cli.Close() jamais appele
```

**`docker/docker.go`** - Pas de méthode Close()
```go
type Docker struct {
    client            *dockerClient.Client  // Fuite de ressource
    // ...
}
// Manque: func (d *Docker) Close() error { return d.client.Close() }
```

---

### 3. Erreurs Silencieusement Ignorées

**`tui/actions.go:101`** - Erreur shell ignorée
```go
t.app.Suspend(func() {
    cmd := exec.Command("docker", "exec", "-it", string(container.ID), "/bin/sh")
    // ...
    _ = cmd.Run()  // Erreur completement ignoree
})
```

**`docker/docker.go:549-553`** - Panic récupéré mais pas remonté
```go
defer func() {
    if r := recover(); r != nil {
        d.logger.ErrorContext(ctx, "panic in handleContainersCommand", slog.Any("recover", r))
        // Pas de reraise, l'erreur est avalee
    }
}()
```

---

### 4. Fuite Potentielle de Context Cancel

**`tui/tui.go:607`** - ctxCancel retourné mais potentiellement perdu
```go
case viewContainerLog:
    ctxCancel = t.refreshContainerLog(ctx, ctxCancel)
    // Si refreshContainerLog retourne un nouveau ctxCancel,
    // l'ancien n'est pas systematiquement annule avant
```

**`tui/tui.go:593-604`** - Annulation conditionnelle insuffisante
```go
case viewProjectList:
    if ctxCancel != nil {
        ctxCancel()
        ctxCancel = nil
    }
    // OK ici, mais le pattern est fragile
```

---

### 5. Data Race sur setTableContainerLogData

**`tui/setup.go:592-596`** - Assignation de référence au lieu de copie
```go
func (t *Tui) setTableContainerLogData(data []string) {
    t.tableContainerLogDataLock.Lock()
    defer t.tableContainerLogDataLock.Unlock()
    t.tableContainerLogData = data  // Reference directe, pas de copie!
}
```

Si l'appelant modifie `data` après l'appel, race condition potentielle.

---

## PROBLEMES MAJEURS

### 6. Shutdown Non Gracieux

**`main.go:46-53`** - Pas de signal de shutdown pour la goroutine Docker
```go
go doc.Run(ctx)

if err := t.Render(ctx); err != nil {
    return fmt.Errorf("failed to render TUI: %w", err)
}

logger.InfoContext(ctx, "c8s is over")
return nil
// La goroutine doc.Run continue potentiellement
```

Le context `ctx` créé à la ligne 21 est `context.Background()` - il n'est jamais annulé!

**`tui/tui.go:936`** - cleanup() ferme le channel mais ne signale pas Docker
```go
func (t *Tui) cleanup() {
    // ...
    close(t.requestData)  // Ferme le channel mais ctx n'est pas annule
}
```

---

### 7. Functors avec Closures Dangereuses

**`docker/docker.go:174-189`** - Closure capturant `containers` par référence
```go
select {
case d.containersCommand <- ContainersCommand{
    functor: func(docker *Docker) *Container {
        for _, c := range docker.containers {
            // ...
            select {
            case container := <-response:
                containers = append(containers, containerResponseToDTO(container))
                // `containers` est capture par reference depuis le scope externe
```

Si le functor est exécuté plus tard, la variable `containers` pourrait être dans un état inattendu.

---

### 8. Duplication de Constantes

**`tui/constants.go:11`** et **`docker/docker.go:397`**
```go
// tui/constants.go
dockerAPITimeout      = 30 * time.Second

// docker/docker.go
const dockerAPITimeout = 30 * time.Second
```

Deux définitions de la même constante. Confusion potentielle si l'une change.

---

### 9. Logs Trop Verbeux en Production

**`tui/tui.go:684`** - Log INFO toutes les 2 secondes
```go
func (t *Tui) refreshContainerLog(ctx context.Context, ctxCancel context.CancelFunc) context.CancelFunc {
    // ...
    t.logger.InfoContext(ctx, "fetching logs for container", slog.String("container_id", currentContainerID))
```

Ce log s'exécute toutes les 2 secondes quand on visualise les logs. Devrait être DEBUG.

---

### 10. Boucle Imbriquée avec Timeouts Potentiellement Longue

**`docker/docker.go:237-254`** - O(n) timeouts dans le pire cas
```go
for _, c := range docker.containers {
    response := make(chan ContainerResponse)
    select {
    case c.Command <- ContainerCommand{response: response}:
    case <-time.After(channelTimeout):  // 5 secondes
        continue
    }

    select {
    case container = <-response:
    case <-time.After(channelTimeout):  // 5 secondes
        continue
    }
    // ...
}
```

Avec 100 containers et des timeouts, cette boucle pourrait prendre jusqu'à 1000 secondes (~16 minutes) dans le pire cas.

---

## PROBLEMES MINEURS

### 11. Magic Strings pour les Statuts

**`tui/tui.go:400-413`** et **`docker/container.go:147-165`**
```go
// tui/tui.go
switch strings.ToLower(statusText) {
case "running":
    statusColor = "green"
case "exited", "dead", "removing":
    statusColor = "red"

// docker/container.go
case events.ActionStart, events.ActionUnPause, events.ActionReload:
    return "running"
case events.ActionStop, events.ActionDie, events.ActionKill, events.ActionOOM:
    return "exited"
```

Ces statuts devraient être des constantes partagées.

---

### 12. Méthodes Getters/Setters Trop Nombreuses

**`tui/setup.go:405-628`** - Plus de 50 méthodes helper
```go
func (t *Tui) setCurrentView(view currentView)
func (t *Tui) getCurrentView() currentView
func (t *Tui) setLogPaused(paused bool)
func (t *Tui) getLogPaused() bool
func (t *Tui) toggleLogPaused()
// ... etc
```

Prolifération de boilerplate. Envisager de regrouper l'état dans des sous-structures avec leurs propres locks.

---

### 13. Struct Tui God Object

**`tui/tui.go:20-81`** - 60+ champs dans une seule struct
```go
type Tui struct {
    app                        *tview.Application
    pages                      *tview.Pages
    // ... 60+ champs
}
```

Devrait être décomposée en sous-composants (ProjectView, ContainerView, LogView).

---

### 14. Tests Absents

Aucun fichier `*_test.go` dans le projet. Pour une application avec autant de concurrence et de logique métier, c'est problématique.

---

### 15. Gestion d'Erreur Incohérente

Certaines erreurs sont loggées en Error, d'autres en Debug, d'autres ignorées:
```go
// Error
d.logger.ErrorContext(ctx, "container logs failed", ...)

// Debug (devrait être Warn?)
d.logger.DebugContext(ctx, "stdcopy finished", slog.Any("error", err))

// Ignoré
_ = cmd.Run()
```

---

### 16. Commentaires Manquants sur les Mutex

**`tui/tui.go:58`** - Bon exemple, mais pas systématique
```go
currentContainerLock       sync.RWMutex // Protects currentProjectID, currentContainerID, ...
```

D'autres locks n'ont pas de documentation sur ce qu'ils protègent.

---

## RESUME DES PROBLEMES

| Catégorie | Critique | Majeur | Mineur |
|-----------|----------|--------|--------|
| Concurrence | 5 | 2 | 0 |
| Ressources | 1 | 2 | 0 |
| Architecture | 0 | 3 | 4 |
| Erreurs | 2 | 0 | 2 |
| Tests | 0 | 0 | 1 |
| **Total** | **8** | **7** | **7** |

---

## RECOMMANDATIONS PRIORITAIRES

### Priorité 1 - Critique
1. **Ajouter des locks pour `projectSearchActive`, `containerSearchActive`, `projectSortColumn`, `projectSortAsc`, `containerSortColumn`, `containerSortAsc`, `currentProjectName`**
2. **Fermer le client Docker proprement avec `defer cli.Close()`**
3. **Copier les données dans `setTableContainerLogData` au lieu d'assigner la référence**
4. **Utiliser un context.WithCancel dans main() et annuler lors du cleanup**

### Priorité 2 - Majeur
5. **Limiter le timeout total des boucles imbriquées dans Docker handlers**
6. **Ajouter une méthode `Close()` à Docker et l'appeler au shutdown**
7. **Uniformiser les constantes (supprimer la duplication `dockerAPITimeout`)**
8. **Passer `logger.InfoContext` à `DebugContext` pour le refresh des logs**

### Priorité 3 - Mineur
9. **Extraire les statuts de containers en constantes partagées**
10. **Décomposer la struct Tui en sous-composants**
11. **Ajouter des tests unitaires (au minimum pour les fonctions critiques de concurrence)**
12. **Documenter les locks avec des commentaires**

---

## POINTS POSITIFS (Améliorations depuis la dernière revue)

- Timeouts ajoutés sur tous les envois channel
- Copie des logs dans `containerToDTO`
- Thread-safe helpers pour les search queries
- Nil check dans `findContainerByService`
- Fermeture explicite de `out` dans `collectContainerLogs`
- Bonne utilisation de `context.Done()` pour la cancellation

---

## CONCLUSION

Le code a progressé de manière significative depuis les revues précédentes. La gestion des timeouts est maintenant robuste et les principales race conditions ont été corrigées. Cependant, plusieurs variables dans la couche TUI restent non protégées, et l'absence de shutdown gracieux reste un problème architectural important.

L'absence totale de tests rend les modifications futures risquées. La structure "god object" de Tui et la prolifération de getters/setters suggèrent qu'une refactorisation architecturale serait bénéfique.

**Note : 7/10** - Fonctionnel et plus fiable qu'avant, mais des problèmes de concurrence persistent et l'architecture mériterait une refonte.
