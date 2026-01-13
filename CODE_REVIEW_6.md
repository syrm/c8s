# Revue de Code Acerbe - c8s (v6)

**Date:** 2026-01-07
**Fichiers analysés:** 14 fichiers Go
**Note globale:** 9/10

---

## Synthèse

Le code a atteint un excellent niveau de qualité. Les problèmes de concurrence critiques ont tous été corrigés. L'architecture est solide avec une séparation claire des responsabilités. Il reste quelques problèmes mineurs de code mort et de verbosité des logs.

---

## Problèmes Critiques (0)

Aucun problème critique identifié. Les data races précédemment identifiées ont été correctement corrigées.

---

## Problèmes Majeurs (0)

Aucun problème majeur identifié.

---

## Problèmes Mineurs (5)

### 1. Fonction `filterContainers` non utilisée (Code mort)

**Fichier:** `tui/sorting.go:56-68`

```go
func filterContainers(containers []model.Container, query string) []model.Container {
    // ...
}
```

**Impact:** Code mort qui alourdit la base de code et peut créer de la confusion.

**Correction:** Supprimer la fonction ou l'utiliser dans `drawContainers`.

---

### 2. Fonction `findContainerByService` non utilisée (Code mort)

**Fichier:** `tui/setup.go:386-403`

```go
func (t *Tui) findContainerByService(rowIndex int) *model.Container {
    // ...
}
```

**Impact:** Code mort identique à `getSelectedContainer` avec un filter différent.

**Correction:** Supprimer la fonction.

---

### 3. Logs INFO trop verbeux dans `tryReconnectContainer`

**Fichier:** `tui/tui.go:764-829`

```go
t.logger.InfoContext(ctx, "checking for container reappearance", ...)
t.logger.InfoContext(ctx, "checking container", ...)
t.logger.InfoContext(ctx, "container reappeared, starting log collection", ...)
t.logger.InfoContext(ctx, "still waiting for logs...")
```

**Problème:** Ces logs de niveau INFO sont appelés toutes les 2 secondes quand un container disparaît. Ils encombrent le fichier de log.

**Correction:** Passer en niveau Debug sauf pour "container reappeared" qui peut rester en INFO.

---

### 4. Log INFO pour fin de stats routine

**Fichier:** `docker/docker.go:585`

```go
d.logger.InfoContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
```

**Problème:** Ce log est émis à chaque fois qu'un container s'arrête normalement (EOF). C'est une opération de routine qui ne devrait pas polluer les logs INFO.

**Correction:** Passer en niveau Debug :
```go
d.logger.DebugContext(ctx, "end of container stats", ...)
```

---

### 5. Absence de contexte dans certaines fonctions de requête

**Fichiers:** `docker/docker.go:224, 258`

```go
func (d *Docker) handleRequestContainerProject(r *model.RequestProject) {
func (d *Docker) handleRequestSetPendingAction(r *model.RequestSetPendingAction) {
```

**Problème:** Ces fonctions n'ont pas de paramètre `context.Context`, ce qui empêche de propager proprement l'annulation pendant les opérations imbriquées sur les channels. Cela pourrait causer des délais lors du shutdown si beaucoup de containers existent.

**Impact:** Mineur car les timeouts empêchent tout blocage permanent.

**Recommandation:** Ajouter un paramètre ctx pour cohérence et meilleure gestion du shutdown :
```go
func (d *Docker) handleRequestContainerProject(ctx context.Context, r *model.RequestProject) {
```

---

## Suggestions d'Amélioration (3)

### 6. Potentielle amélioration de performance dans handleRequestProjectList

**Fichier:** `docker/docker.go:287-352`

Le functor itère sur tous les containers et envoie une commande à chacun séquentiellement. Avec beaucoup de containers, cela pourrait créer des latences.

**Suggestion:** Pour l'instant acceptable car les timeouts limitent l'impact. À surveiller si le nombre de containers augmente significativement.

---

### 7. Pattern de timeout dupliqué

Le pattern suivant est répété ~15 fois dans le code :
```go
select {
case channel <- value:
case <-time.After(timeout):
    // log warning
case <-ctx.Done():
    return
}
```

**Suggestion:** Considérer une fonction helper générique (optionnel, amélioration cosmétique).

---

### 8. Constantes de timeout potentiellement configurables

**Fichiers:** `docker/docker.go:452-456`, `tui/constants.go:6-11`

Les timeouts sont hardcodés. Pour une utilisation en production avec des environnements variés, ils pourraient être configurables via variables d'environnement.

**Suggestion:** Optionnel pour une v1, à considérer pour une v2.

---

## Points Positifs

1. **Architecture exemplaire** : Séparation claire TUI/Docker avec communication par channels
2. **Aucun data race** : Le pattern command channel est correctement appliqué partout
3. **Timeouts systématiques** : Aucune opération channel ne peut bloquer indéfiniment
4. **Shutdown gracieux** : Le pattern `done` channel + `Wait()` est bien implémenté
5. **Copie des données** : Les slices sont correctement copiées pour éviter les races
6. **Protection des états partagés** : RWMutex utilisés de manière cohérente dans la couche TUI
7. **Gestion des erreurs** : Les erreurs Docker sont correctement gérées et loggées
8. **Recovery panic** : `handleContainersCommand` a un recover qui convertit en erreur
9. **Nettoyage des ressources** : `cleanup()` ferme proprement les timers et channels
10. **Code lisible** : Fonctions bien découpées avec des noms explicites

---

## Statistiques

| Catégorie | Nombre |
|-----------|--------|
| Problèmes critiques | 0 |
| Problèmes majeurs | 0 |
| Problèmes mineurs | 5 |
| Suggestions | 3 |

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

### Analyse des Champs Partagés (TUI)

| Champ | Protection | Status |
|-------|------------|--------|
| `tableProjectData` | `tableProjectDataLock` | ✅ OK |
| `tableContainerData` | `tableContainerDataLock` | ✅ OK |
| `tableContainerLogData` | `tableContainerLogDataLock` | ✅ OK |
| `currentView` | `currentViewLock` | ✅ OK |
| `currentProjectID/ContainerID/etc` | `currentContainerLock` | ✅ OK |
| `projectSortColumn/Asc` | `projectSortLock` | ✅ OK |
| `containerSortColumn/Asc` | `containerSortLock` | ✅ OK |
| `logPaused` | `logPausedLock` | ✅ OK |
| `logFilter` | `logFilterLock` | ✅ OK |
| `logShowTimestamp` | `logShowTimestampLock` | ✅ OK |
| `containerDisappeared` | `containerDisappearedLock` | ✅ OK |
| `projectRefreshPaused` | `projectRefreshPausedLock` | ✅ OK |
| `containerRefreshPaused` | `containerRefreshPausedLock` | ✅ OK |
| `statusTimer` | tview event loop | ✅ OK (serialized) |

---

## Conclusion

Le code est maintenant de **qualité production**. Les problèmes de concurrence ont été méthodiquement corrigés au fil des revues. Les 5 problèmes mineurs restants sont principalement du code mort et des ajustements de niveau de log qui n'affectent pas la stabilité ou les performances.

**Priorité de correction (optionnel):**
1. Supprimer `filterContainers` (code mort)
2. Supprimer `findContainerByService` (code mort)
3. Passer les logs verbeux en Debug
4. Ajouter contexte aux fonctions de requête (amélioration)

**Évolution de la note:**
- CODE_REVIEW_3: 7/10
- CODE_REVIEW_4: 8/10
- CODE_REVIEW_5: 8.5/10
- CODE_REVIEW_6: **9/10**

Le projet est prêt pour une utilisation en production.
