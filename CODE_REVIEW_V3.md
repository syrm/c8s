# RAPPORT DE REVUE DE CODE - C8S V3
## Analyse approfondie et critique du projet Go

**Date**: 2026-01-13
**Réviseur**: Claude Opus 4.5

---

## 1. PROBLÈMES CRITIQUES

### 1.1 Race Condition sur `statsGen` dans `docker/docker.go`
**Fichier**: `docker/docker.go`, lignes 984-990, 1076-1080
**Sévérité**: CRITIQUE

**Problème**: La variable `statsGen` est incrémentée et utilisée sans synchronisation adéquate:

```go
// Ligne 984-990: Dans createContainer()
currentGen := c.statsGen.Load()
d.statsWG.Add(1)
go func() {
    defer d.statsWG.Done()
    d.getContainerStatsRealtime(statsCtx, c, currentGen)
}()

// Ligne 1260: Dans handleEvents()
newGen := c.statsGen.Add(1)
```

Le problème: Il existe une fenêtre entre le `Load()` et la création de la goroutine où `statsGen` peut être modifié par une autre goroutine. Bien que le code capte la génération avant le `Add(1)`, l'ordre d'exécution des goroutines n'est pas garanti, ce qui peut causer que deux goroutines avec des générations différentes traitent des données en même temps.

**Solution proposée**:
- Utiliser un `sync.Mutex` autour des opérations atomiques sur `statsGen`
- Ou mieux: utiliser un pattern de génération basé sur `sync.Cond` pour notifier les anciennes goroutines de se terminer

---

### 1.2 Deadlock potentiel dans `handleRequestContainerLog()`
**Fichier**: `docker/docker.go`, lignes 257-395
**Sévérité**: CRITIQUE

**Problème**: La fonction `handleRequestContainerLog` crée un deadlock en raison de deux problèmes:

1. **Timeouts décalés**: Les timers sont créés à des moments différents mais utilisés de manière non-synchronisée
2. **Logique compliquée de réception**: Entre la ligne 308-346, le code crée une functor qui modifie le `logCollectionActive` et envoie un signal sur `startCollection`, mais attend la réception sur `dtoResponse` AVANT de recevoir sur `startCollection`. Cela signifie:
   - Le code envoie à `c.Command` à la ligne 308
   - Puis attend `<-response` à la ligne 352
   - Mais la functor doit d'abord envoyer sur `dtoResponse` PUIS sur `startCollection`
   - Si le `handleCommands()` du container est bloqué, deadlock

```go
// Problématique: ordre d'envoi
dtoResponse <- dtoContainer   // Ligne 328
startCollection <- needStart  // Ligne 336

// Problématique: ordre de réception
case dtoContainer = <-dtoResponse:  // Attend ici ligne 353
case needStart := <-startCollection: // Mais envoie aussi ici ligne 367
```

**Solution proposée**:
- Simplifier la logique en deux étapes distinctes
- S'assurer que tous les channels internes sont bufferisés (1 minimum)
- Utiliser un seul `select` avec `context.Done()` plutôt que de cascader les timers

---

### 1.3 Fuite de contexte avec `statsCtx` créé avec `errCtx`
**Fichier**: `docker/docker.go`, lignes 1253-1270
**Sévérité**: CRITIQUE

**Problème**: Dans `handleEvents()`, lorsqu'on redémarre la collection des stats:

```go
// Ligne 1253-1270
if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
    d.statsContextsLock.Lock()
    if oldCancel, exists := d.statsContexts[c.ID]; exists {
        oldCancel()
    }
    newGen := c.statsGen.Add(1)
    statsCtx, statsCancel := context.WithCancel(ctx)  // PROBLÈME: ctx est errCtx du Run()
    d.statsContexts[c.ID] = statsCancel
    d.statsContextsLock.Unlock()

    d.statsWG.Add(1)
    go func(gen uint64) {
        defer d.statsWG.Done()
        d.getContainerStatsRealtime(statsCtx, c, gen)
    }(newGen)
}
```

**Problèmes**:
1. Le `statsCtx` est créé avec `ctx` (qui est `errCtx` du `errgroup`), ce qui signifie que lorsque le premier goroutine retourne une erreur, TOUS les `statsCtx` deviennent invalides
2. La race condition entre `oldCancel()` et la création d'un nouveau contexte

**Solution proposée**: Créer chaque `statsCtx` avec un parent dédié qui n'est pas le context du Run()

---

## 2. PROBLÈMES IMPORTANTS

### 2.1 Pattern de cascading timers répété 50+ fois
**Fichier**: `docker/docker.go`, partout
**Sévérité**: IMPORTANT

**Problème**: Le pattern utilisé partout dans le code Docker:

```go
timer1 := time.NewTimer(model.ChannelTimeout)
select {
case d.containersCommand <- cmd:
    timer.Stop(timer1)
case <-timer1.C:
    // Timeout
case <-ctx.Done():
    timer.Stop(timer1)
}

var c *Container
timer2 := time.NewTimer(model.ChannelTimeout)  // NOUVEAU TIMER
select {
case c = <-response:
    timer.Stop(timer2)
case <-timer2.C:
    // Timeout
case <-ctx.Done():
    timer.Stop(timer2)
}
```

**Problèmes**:
1. **Création répétée de timers**: Chaque opération crée un nouveau timer - gaspille des ressources et crée beaucoup d'allocations heap
2. **Oublis de Stop()**: Par exemple à la ligne 1177 dans `handleEvents()`, le code ne fait `timer.Stop(timer1)` que parfois
3. **Logique verbose et répétitive**: Le même pattern est répété 50+ fois dans le fichier

**Solution proposée**:
- Créer une fonction utilitaire `sendWithTimeout(ctx, ch, msg) error` qui gère le timeout
- Ou utiliser un pattern de context avec timeout plutôt que des timers explicites

---

### 2.2 `LogCollectionActive` n'est pas thread-safe
**Fichier**: `docker/container.go`, lignes 28, 331-334
**Sévérité**: IMPORTANT

**Problème**: Le champ `LogCollectionActive` est accédé et modifié sans synchronisation:

```go
type Container struct {
    // ...
    LogCollectionActive bool  // Pas de protection!
    // ...
}

// Ligne 331-334 dans docker.go
needStart := !container.LogCollectionActive
if needStart {
    container.LogCollectionActive = true
}
```

Cela se fait à l'intérieur d'une functor qui s'exécute dans `handleCommands()`, mais:
1. Le champ est accessible depuis plusieurs goroutines
2. Il n'y a aucune atomicité garantie

**Solution proposée**: Utiliser `atomic.Bool` comme pour `deleted`

---

### 2.3 Logique de panic recovery confuse
**Fichier**: `docker/docker.go`, lignes 1119-1148
**Sévérité**: IMPORTANT

**Problème**: Le code essaie de récupérer les panics:

```go
defer func() {
    if r := recover(); r != nil {
        d.logger.ErrorContext(ctx, "panic in containersCommand functor", slog.Any("recover", r))
        c = nil
    }
    // Nested recovery to handle closed channel panic
    if cmd.response != nil {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    d.logger.ErrorContext(ctx, "panic sending response (channel likely closed)", slog.Any("recover", r))
                }
            }()
            cmd.response <- c
        }()
    }
}()
```

**Problèmes**:
1. Envoyer sur un channel fermé dans le second recover ne fera jamais panic - c'est déjà après le defer
2. Le vrai problème est: **pourquoi les functors paniqueraient-ils?** Si c'est une possibilité, le code est très instable
3. La logique est confuse et défensive, mais ne résout pas le problème root

**Solution proposée**:
- Ne pas essayer de catcher les panics - c'est un code smell
- Si les functors peuvent panicker, l'architecture est mauvaise
- Utiliser plutôt une approche "fail-safe" où les erreurs retournent des valeurs au lieu de panicker

---

### 2.4 Locks tenus trop longtemps
**Fichier**: `docker/docker.go`, lignes 1226-1231, 1254-1264
**Sévérité**: IMPORTANT

**Problème**: Les locks sur les maps de contextes sont tenus pendant de longues opérations:

```go
// Ligne 1254-1264: Dans handleEvents()
d.statsContextsLock.Lock()
if oldCancel, exists := d.statsContexts[c.ID]; exists {
    oldCancel()
}
newGen := c.statsGen.Add(1)
statsCtx, statsCancel := context.WithCancel(ctx)
d.statsContexts[c.ID] = statsCancel
d.statsContextsLock.Unlock()  // Lock tenu longtemps
```

Le pattern correct en Go est:
1. Acquérir le lock
2. Faire le minimum nécessaire
3. Relâcher le lock

**Solution proposée**: Minimiser la section critique

---

## 3. PROBLÈMES MODÉRÉS

### 3.1 Channels requests pas assez bufferisés
**Fichier**: `docker/docker.go`
**Sévérité**: MODÉRÉ

**Problème**:
```go
containersCommand: make(chan ContainersCommand, 16),  // Seulement 16
```

Si 17 goroutines envoient simultanément, le code **dépend entièrement des timeouts** pour ne pas bloquer. C'est fragile.

---

### 3.2 Validation d'ID faible
**Fichier**: `tui/actions.go`, lignes 25-35
**Sévérité**: MODÉRÉ

**Problème**: Le validateur est très basique:
```go
func isValidContainerID(id string) bool {
    if len(id) < 12 || len(id) > 64 {
        return false
    }
    for _, c := range id {
        if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
            return false
        }
    }
    return true
}
```

**Problèmes**:
1. Accepte les IDs en majuscules, mais Docker les retourne en minuscules - peut causer des mismatches
2. N'y a aucune vérification que l'ID appartient réellement au container sélectionné
3. Un attaquant pourrait potentiellement construire un ID valide en syntaxe mais pas en sémantique

**Solution proposée**: Comparer directement avec `container.ID` au lieu de valider la syntaxe

---

### 3.3 Pas de gestion de signaux explicite
**Fichier**: `main.go`, lignes 20-62
**Sévérité**: MODÉRÉ

**Problème**: Le code crée des goroutines et des channels, mais n'intercepte pas les signaux:

```go
func run() error {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    // ...
    go doc.Run(ctx)
    if err := t.Render(ctx); err != nil {
        return fmt.Errorf("failed to render TUI: %w", err)
    }
    cancel()
    doc.Wait()
}
```

Si l'utilisateur fait Ctrl+C AVANT que `t.Render()` retourne, le contexte n'est pas annulé correctement.

---

### 3.4 Gestion d'erreurs incohérente
**Fichier**: Partout dans `docker/docker.go`
**Sévérité**: MODÉRÉ

**Problème**: Le code utilise des patterns mélangés:

1. Parfois ignore les erreurs silencieusement:
```go
logStream, err := d.openLogStream(ctx, c)
if err != nil {
    return  // Silencieux
}
```

2. Parfois log et continue:
```go
d.logger.ErrorContext(ctx, "container stats failed", ...)
```

3. Parfois retourne l'erreur:
```go
if err != nil {
    return fmt.Errorf("listing containers: %w", err)
}
```

**Solution proposée**: Choisir une stratégie d'erreur cohérente et documenter quelles erreurs sont "attendues"

---

### 3.5 Maps contextes sans limite de taille
**Fichier**: `docker/docker.go`, lignes 49-54
**Sévérité**: MODÉRÉ

**Problème**: Les maps `statsContexts` et `logContexts` peuvent croître indéfiniment:

```go
statsContexts     map[model.ContainerID]context.CancelFunc
logContexts       map[model.ContainerID]context.CancelFunc
```

Si des containers sont créés et détruits très rapidement, on pourrait avoir une fuite mémoire.

---

## 4. PROBLÈMES MINEURS

### 4.1 Timer jamais arrêté après timeout
**Fichier**: `docker/docker.go`, lignes 204-210 dans `handleEventsWithBackoff()`
**Sévérité**: MINEUR

```go
backoffTimer := time.NewTimer(backoff)
select {
case <-ctx.Done():
    timer.Stop(backoffTimer)
    return
case <-backoffTimer.C:
}
// LE TIMER N'EST JAMAIS ARRÊTÉ ICI!
```

---

### 4.2 Pas de logging pour les messages perdus
**Fichier**: `tui/tui.go`, lignes 540-545
**Sévérité**: MINEUR

```go
select {
case t.requestData <- req:
    return true
case <-timer.C:
    return false  // Silencieusement
}
```

---

### 4.3 Pas de limite de taille en bytes pour les logs
**Fichier**: `docker/container.go`, lignes 156-175
**Sévérité**: MINEUR

Il y a une limite de 1000 lignes mais pas de limite de taille en bytes. Une ligne très longue (10MB) peut causer une fuite mémoire.

---

### 4.4 Validation ProjectID trop permissive
**Fichier**: `docker/docker.go`, lignes 879-892
**Sévérité**: MINEUR

```go
func isValidProjectID(projectID string) bool {
    // Permet les espaces et autres caractères bizarres
    // Pas de limite de longueur
}
```

---

### 4.5 Pas de documentation sur les invariants de concurrence
**Fichier**: Partout
**Sévérité**: MINEUR

Le code a des patterns de concurrence subtils mais pas de documentation claire sur quel state est protégé par quel lock.

---

### 4.6 `fmt.Sprintf` pour les logs au lieu de structured logging
**Fichier**: `tui/actions.go`, lignes 126, 181, 240
**Sévérité**: MINEUR

```go
t.showStatusMessage(fmt.Sprintf("Failed to stop container: %v", err))
```

---

### 4.7 Pas de tests
**Fichier**: Aucun `*_test.go`
**Sévérité**: MINEUR (mais idéalement IMPORTANT)

Zéro tests. Avec cette complexité de concurrence, c'est dangereux.

---

### 4.8 Code potentiellement mort
**Fichier**: `tui/log_formatter.go`, lignes 26-44
**Sévérité**: MINEUR

Le type `commonLogFormat` est défini mais ses méthodes ne sont jamais utilisées - le code préfère `getStringField()`.

---

### 4.9 Fonction `tryReconnectContainer()` trop complexe
**Fichier**: `tui/tui.go`, lignes 682-752
**Sévérité**: MINEUR

70 lignes avec beaucoup de requêtes réseau et timeouts imbriqués. Difficile à maintenir.

---

### 4.10 Constante `maxConcurrentActions` non documentée
**Fichier**: `tui/constants.go`
**Sévérité**: MINEUR

Pas de documentation sur pourquoi cette valeur a été choisie.

---

## RÉSUMÉ

| Sévérité | Nombre |
|----------|--------|
| CRITIQUE | 3 |
| IMPORTANT | 4 |
| MODÉRÉ | 5 |
| MINEUR | 10 |
| **TOTAL** | **22** |

---

## RECOMMANDATIONS PRIORITAIRES

1. **Refactoriser `handleRequestContainerLog()` IMMÉDIATEMENT** - Trop complex, trop de timers, trop de possibilités de deadlock

2. **Utiliser `atomic.Bool` pour `LogCollectionActive`** - Simple fix pour un problème réel

3. **Créer fonction utilitaire `sendWithTimeout()`** - Éliminera 50+ lignes de code boilerplate

4. **Séparer le contexte des stats/logs du errgroup context** - Éviter les annulations en cascade

5. **Ajouter tests de concurrence** - Au minimum test les race conditions avec `-race`

6. **Documenter les invariants de concurrence** - Ajouter des commentaires explicites sur quel état est protégé par quel lock
