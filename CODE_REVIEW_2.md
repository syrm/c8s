# Revue de Code Acerbe - c8s

## Note Globale : 6/10

Le code a été amélioré depuis la dernière revue, mais plusieurs problèmes critiques persistent.

---

## 🔴 PROBLÈMES CRITIQUES

### 1. Race Conditions Persistantes

**`tui/header.go:7-8` et `111`** - Accès non protégé à `currentProjectID` et `currentContainerService`
```go
func (t *Tui) getProjectName() string {
    if t.currentProjectID == "" {  // ❌ Accès direct sans lock
        return "unknown"
    }
    // ...
    if string(project.ID) == t.currentProjectID {  // ❌ Encore ici
```

```go
func (t *Tui) buildLogViewHeader() string {
    // ...
    return fmt.Sprintf("... %s ...", t.currentContainerService)  // ❌ Accès direct
```

**`tui/setup.go:391`** - Accès à `GetCell` sans nil check dans `findContainerByService`
```go
cellText := t.tableContainer.GetCell(rowIndex, 0).Text  // ❌ Panic si nil
```

**`tui/tui.go:294,376`** - `projectSearchQuery` et `containerSearchQuery` accédés sans lock
```go
projects = filterProjects(projects, t.projectSearchQuery)  // ❌ Race condition
// ...
if !fuzzyMatch(container.Service, t.containerSearchQuery) {  // ❌ Race condition
```

**`tui/setup.go:76-77,120,160-168`** - Mêmes variables accédées sans protection
```go
t.projectSearchActive = true
t.projectSearchInput.SetText(t.projectSearchQuery)  // ❌ Race
```

---

### 2. Deadlock Potentiel dans Docker Layer

**`docker/docker.go:122-131`** - Pas de timeout sur l'envoi au channel `containersCommand`
```go
func (d *Docker) handleRequestContainerLog(...) *Container {
    response := make(chan *Container)
    d.containersCommand <- ContainersCommand{...}  // ❌ Bloque indéfiniment
    c := <-response  // ❌ Bloque indéfiniment
```

Si `handleContainersCommand` est bloqué ou terminé, cela crée un deadlock. Toutes les fonctions `handleRequest*` ont ce problème.

**`docker/docker.go:148-163`, `166-184`, `187-235`** - Même problème dans toutes les fonctions handler.

---

### 3. Goroutine Leaks

**`docker/docker.go:276-283`** - Goroutine stdcopy peut rester orpheline
```go
go func() {
    defer pw.Close()
    _, err := stdcopy.StdCopy(pw, pw, out)  // ❌ Si out n'est jamais fermé, bloque indéfiniment
    // ...
}()
```

Si le contexte est annulé mais que `out` (le reader Docker) ne se ferme pas immédiatement, cette goroutine reste en vie.

**`docker/docker.go:402-406, 412-419`** - Envoi sur channel potentiellement bloqué
```go
c.Command <- ContainerCommand{...}  // ❌ Si container.handleCommands est terminé, bloque indéfiniment
```

---

### 4. Fuite de Context

**`docker/container.go:54`** - Context créé mais jamais utilisé pour les goroutines enfants
```go
ctx, cancel := context.WithCancel(ctx)
// ctx est passé à handleCommands mais pas aux goroutines de stats/logs
```

**`docker/docker.go:100`** - Context créé dans handleRequests mais `cancel` stocké dans DTO
```go
ctxLog, cancel := context.WithCancel(ctx)
// cancel est passé au TUI via DTO, mais si le TUI ne l'appelle jamais...
```

---

## 🟠 PROBLÈMES MAJEURS

### 5. Incohérence de Synchronisation

**`tui/tui.go:69`** - `currentProjectName` n'a pas de mutex dédié mais est accédé partout
```go
currentProjectName         string  // ❌ Pas de lock, accédé dans enterContainerView
```

**`tui/setup.go:120`** - Accès mixte protégé/non-protégé
```go
t.setCurrentProjectID(string(project.ID))  // ✓ Protégé
t.currentProjectName = project.Name         // ❌ Non protégé
```

### 6. Gestion d'Erreurs Silencieuse

**`tui/actions.go:101`** - Erreur ignorée
```go
_ = cmd.Run()  // ❌ Erreur shell silencieusement ignorée
```

**`docker/docker.go:279-281`** - Log debug pour une erreur potentiellement critique
```go
if err != nil && err != io.EOF {
    d.logger.DebugContext(...)  // ❌ Devrait être au moins Info/Warn
}
```

### 7. Copie de Slice Inefficace et Incomplète

**`tui/setup.go:579-585`** - Copie superficielle pour strings (ok), mais...
```go
func (t *Tui) getTableContainerLogData() []string {
    result := make([]string, len(t.tableContainerLogData))
    copy(result, t.tableContainerLogData)
    return result
}
```

**`docker/dto_mapper.go:23`** - Copie directe de slice de logs sans protection
```go
d.Logs = c.Logs  // ❌ Référence directe, pas de copie
```

Si le container continue d'ajouter des logs pendant que le TUI lit `c.Logs`, race condition.

---

### 8. Pattern Functor Anti-Pattern Persistant

**`docker/docker.go:25-28, 42-45`** - Architecture complexe et fragile
```go
type ContainersCommand struct {
    functor  func(*Docker) *Container  // ❌ Fonctions anonymes difficiles à debugger
    response chan *Container
}
```

Ce pattern rend le code difficile à tester, maintenir et debugger. Une simple interface serait plus claire.

---

## 🟡 PROBLÈMES MINEURS

### 9. Timeouts Incohérents

**`tui/constants.go:10-11`** - Deux constantes de timeout avec des noms similaires
```go
channelTimeout        = 5 * time.Second
dockerAPITimeout      = 30 * time.Second  // ❌ Inutilisé côté TUI, confusion
```

**`docker/docker.go:335`** - `dockerAPITimeout` défini ici aussi (duplication)

### 10. Magic Strings

**`tui/tui.go:388-410`** - Statuts en dur
```go
case "running":
    statusColor = "green"
case "exited", "dead", "removing":
```

Devraient être des constantes.

### 11. Struct God Object

**`tui/tui.go:20-78`** - 58 champs dans la struct Tui
```go
type Tui struct {
    // 58 champs...
}
```

Devrait être décomposée en sous-structs (ProjectView, ContainerView, LogView, etc.).

---

### 12. Tests Absents

Aucun test unitaire. Pour du code avec autant de concurrence, c'est inacceptable.

---

### 13. Channel Non Bufferisé

**`docker/docker.go:53`** - containersCommand non bufferisé
```go
containersCommand: make(chan ContainersCommand),  // ❌ Bloquant
```

Tous les envois bloquent jusqu'à réception.

**`docker/container.go:77`** - Command channel non bufferisé
```go
Command: make(chan ContainerCommand),  // ❌ Bloquant
```

---

### 14. Absence de Graceful Shutdown

**`docker/docker.go:59-83`** - Pas de nettoyage des containers à la fermeture
```go
func (d *Docker) Run(ctx context.Context) {
    // Pas de cleanup des goroutines de stats/logs
    // Pas de fermeture du client Docker
}
```

**`tui/tui.go:924-935`** - cleanup() ferme le channel mais ne garantit pas que toutes les goroutines sont terminées
```go
func (t *Tui) cleanup() {
    close(t.requestData)  // ❌ Peut causer des panics si des goroutines envoient encore
}
```

---

### 15. Logs Verbeux en Production

**`tui/tui.go:682`** - Log Info à chaque refresh (toutes les 2 secondes)
```go
t.logger.InfoContext(ctx, "fetching logs for container", ...)  // ❌ Spam
```

---

## 📊 Résumé des Problèmes

| Catégorie | Critique | Majeur | Mineur |
|-----------|----------|--------|--------|
| Concurrence | 4 | 2 | 2 |
| Ressources | 2 | 1 | 1 |
| Architecture | 0 | 1 | 3 |
| Erreurs | 0 | 1 | 0 |
| Tests | 0 | 0 | 1 |
| **Total** | **6** | **5** | **7** |

---

## 🎯 Recommandations Prioritaires

1. **Ajouter des locks pour `projectSearchQuery`, `containerSearchQuery`, `currentProjectName`**
2. **Utiliser `getCurrentProjectID()` partout (header.go notamment)**
3. **Ajouter des timeouts sur TOUS les envois channel côté Docker**
4. **Copier `c.Logs` dans `containerToDTO` pour éviter les race conditions**
5. **Bufferiser les channels critiques**
6. **Implémenter un graceful shutdown propre avec sync.WaitGroup**
7. **Ajouter des tests de concurrence avec `-race`**
8. **Corriger le nil check manquant dans `findContainerByService`**

---

## Conclusion

Le code s'est amélioré mais reste fragile. Les problèmes de concurrence sont nombreux et pourraient causer des crashes en production. L'absence de tests rend toute modification risquée. L'architecture "functor" ajoute de la complexité inutile.

**Note : 6/10** - Fonctionnel mais non fiable pour un usage intensif.
