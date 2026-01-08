# Revue de Code Acerbe - c8s (v5)

**Date:** 2026-01-07
**Fichiers analysés:** 14 fichiers Go
**Note globale:** 8.5/10

---

## Synthese

Le code a nettement progressé depuis les revues précédentes. Les problèmes critiques ont été corrigés (fermeture du client Docker, gestion du shutdown gracieux, protection des variables partagées). Il reste quelques problèmes de concurrence subtils et des améliorations mineures à apporter.

---

## Problemes Critiques (0)

Aucun problème critique identifié.

---

## Problemes Majeurs (2)

### 1. Data Race: Lecture de `LogCollectionActive` hors du pattern Command Channel

**Fichier:** `docker/docker.go:164`

```go
if c != nil && !c.LogCollectionActive {  // <-- LECTURE DIRECTE NON SYNCHRONISEE
    select {
    case c.Command <- ContainerCommand{
        functor: func(container *Container) {
            container.LogCollectionActive = true
        },
    }:
```

**Problème:** Le champ `LogCollectionActive` du Container est lu directement sans passer par le pattern command channel. Pendant ce temps, `collectContainerLogs` peut modifier ce champ via un functor dans une autre goroutine.

**Correction:** Lire le champ via le command channel :
```go
// Envoyer une commande qui vérifie et démarre la collection atomiquement
select {
case c.Command <- ContainerCommand{
    functor: func(container *Container) {
        if !container.LogCollectionActive {
            container.LogCollectionActive = true
            go d.collectContainerLogs(ctx, container)
        }
    },
}:
```

---

### 2. Data Race: Lecture de `c.Logs` dans `containerToDTO`

**Fichier:** `docker/dto_mapper.go:24-25`

```go
d.Logs = make([]string, len(c.Logs))  // len(c.Logs) lu sans synchro
copy(d.Logs, c.Logs)                   // slice header lu sans synchro
```

**Problème:** Cette fonction lit `c.Logs` directement alors que `collectContainerLogs` peut y ajouter des lignes via `AppendLog` au même moment. Même si `copy()` est utilisé, la lecture de la longueur du slice et de son contenu n'est pas atomique.

**Contexte:** `containerToDTO` est appelé depuis `handleRequestContainerLog` (ligne 122) après avoir obtenu le container via le command channel, mais les logs peuvent être modifiés concurremment.

**Correction:** Déplacer la conversion DTO à l'intérieur d'un functor :
```go
// Dans handleRequestContainerLog, remplacer containerToDTO par:
select {
case c.Command <- ContainerCommand{
    functor: func(container *Container) {
        dto := dto.Container{
            ID: dto.ContainerID(container.ID),
            // ... copier tous les champs
            Logs: make([]string, len(container.Logs)),
        }
        copy(dto.Logs, container.Logs)
        dto.LogCancel = cancel
        r.Response <- dto
    },
}:
```

---

## Problemes Mineurs (5)

### 3. Fonction `filterContainers` non utilisée

**Fichier:** `tui/sorting.go:56-68`

```go
func filterContainers(containers []dto.Container, query string) []dto.Container {
    // ...
}
```

**Impact:** Code mort qui alourdit la base de code.

**Correction:** Supprimer la fonction ou l'utiliser dans `drawContainers`.

---

### 4. Accès mixte tview/data locks dans les handlers

**Fichier:** `tui/setup.go:111-125`

```go
t.tableProjectDataLock.RLock()
for _, project := range t.tableProjectData {
    cell := t.tableProject.GetCell(rowIndex, 0)  // <-- Accès tview sous lock
    // ...
}
t.tableProjectDataLock.RUnlock()
```

**Problème:** Mélange d'opérations tview avec des locks de données. Même si cela fonctionne car le handler est sur le thread tview, ce pattern est fragile.

**Recommandation:** Copier les données sous lock, puis effectuer les opérations tview sans lock :
```go
t.tableProjectDataLock.RLock()
projectsCopy := make([]dto.Project, 0, len(t.tableProjectData))
for _, p := range t.tableProjectData {
    projectsCopy = append(projectsCopy, p)
}
t.tableProjectDataLock.RUnlock()

// Opérations tview sans lock
for _, project := range projectsCopy {
    cell := t.tableProject.GetCell(rowIndex, 0)
    // ...
}
```

**Fichiers concernés:**
- `tui/setup.go:111-125` (enterContainerView)
- `tui/setup.go:231-243` (enterLogView)
- `tui/actions.go:28-56` (getSelectedContainer)

---

### 5. Timer `statusTimer` sans protection

**Fichier:** `tui/tui.go:500-516`

```go
func (t *Tui) showStatusMessage(message string) {
    // ...
    if t.statusTimer != nil {  // Lecture sans lock
        t.statusTimer.Stop()
    }
    t.statusTimer = time.AfterFunc(...)  // Écriture sans lock
}
```

**Problème:** Le champ `statusTimer` est accédé sans synchronisation. Bien que la fonction soit principalement appelée depuis des callbacks tview, cela reste un risque si appelée depuis plusieurs endroits.

**Correction:** Ajouter un mutex pour statusTimer :
```go
type Tui struct {
    // ...
    statusTimer     *time.Timer
    statusTimerLock sync.Mutex
}

func (t *Tui) showStatusMessage(message string) {
    // ...
    t.statusTimerLock.Lock()
    if t.statusTimer != nil {
        t.statusTimer.Stop()
    }
    t.statusTimer = time.AfterFunc(...)
    t.statusTimerLock.Unlock()
}
```

---

### 6. Logs INFO verbeux dans `tryReconnectContainer`

**Fichier:** `tui/tui.go:764-829`

```go
t.logger.InfoContext(ctx, "checking for container reappearance", ...)
t.logger.InfoContext(ctx, "checking container", ...)
t.logger.InfoContext(ctx, "container reappeared, starting log collection", ...)
t.logger.InfoContext(ctx, "still waiting for logs...")
```

**Problème:** Ces logs de niveau INFO sont trop fréquents pour des opérations de routine (appelées toutes les 2 secondes). Ils encombrent le fichier de log.

**Correction:** Passer en niveau Debug :
```go
t.logger.DebugContext(ctx, "checking for container reappearance", ...)
```

---

### 7. Duplication du pattern de timeout channel

**Fichiers:** `docker/docker.go`, `tui/tui.go`

Le pattern suivant est dupliqué une quinzaine de fois :
```go
select {
case channel <- value:
case <-time.After(channelTimeout):
    // log warning
    return
case <-ctx.Done():
    return
}
```

**Recommandation:** Créer une fonction helper générique (optionnel, amélioration de lisibilité) :
```go
func sendWithTimeout[T any](ctx context.Context, ch chan<- T, value T, timeout time.Duration) bool {
    select {
    case ch <- value:
        return true
    case <-time.After(timeout):
        return false
    case <-ctx.Done():
        return false
    }
}
```

---

## Suggestions d'Amelioration (3)

### 8. Pré-allocation des slices dans les filtres

**Fichier:** `tui/sorting.go:41-53`

```go
filtered := make([]dto.Project, 0, len(projects))  // Bonne pratique, déjà fait
```

Ceci est déjà bien implémenté.

---

### 9. Utiliser `strings.EqualFold` pour comparaison case-insensitive

**Fichier:** `tui/sorting.go:12-38`

Le fuzzy match convertit les deux chaînes en lowercase. Pour des comparaisons simples, `strings.EqualFold` serait plus efficace, mais le fuzzy match a une logique différente donc c'est correct.

---

### 10. Documentation des fonctions exportées

Les fonctions exportées comme `NewTui`, `NewDocker`, `Render`, `Run`, `Wait`, `Close` sont bien documentées. Bon travail.

---

## Points Positifs

1. **Architecture propre** : Séparation claire entre couches TUI et Docker avec communication par channels
2. **Pattern Command Channel** : Utilisé correctement pour la plupart des accès concurrents
3. **Gestion du shutdown** : Implémentation propre avec `done` channel et `Wait()`
4. **Protection des données TUI** : RWMutex bien utilisés pour les caches de données
5. **Copie des données** : `getTableContainerLogData` et `setTableContainerLogData` copient correctement
6. **Timeouts partout** : Aucune opération channel ne bloque indéfiniment
7. **Logs structurés** : Utilisation de slog avec contexte

---

## Statistiques

| Catégorie | Nombre |
|-----------|--------|
| Problèmes critiques | 0 |
| Problèmes majeurs | 2 |
| Problèmes mineurs | 5 |
| Suggestions | 3 |

---

## Conclusion

Le code est de bonne qualité avec une architecture solide. Les deux problèmes majeurs restants sont des data races subtiles dans la couche Docker qui doivent être corrigées pour garantir la stabilité en production. Les problèmes mineurs sont principalement cosmétiques ou des améliorations de robustesse.

**Priorité de correction:**
1. Data race `LogCollectionActive` (MAJEUR)
2. Data race `containerToDTO` (MAJEUR)
3. Timer `statusTimer` sans protection (MINEUR)
4. Supprimer `filterContainers` non utilisé (MINEUR)
5. Réduire verbosité des logs (MINEUR)
