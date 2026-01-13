# RAPPORT DE REVUE DE CODE CRITIQUE - Projet c8s (V2)

**Date:** 2026-01-12
**Analyseur:** Claude Code Review - Mode Acerbe

---

## Résumé Exécutif

Le projet c8s est une application TUI Go pour monitorer Docker Compose. Bien que bien structuré globalement, cette revue identifie **37 problèmes critiques ou importants** affectant la concurrence, la gestion des ressources, et les bonnes pratiques Go. De nombreux problèmes subtils pourraient causer des race conditions, fuites de ressources, ou deadlocks en production.

**Verdict: Le code n'est PAS prêt pour la production sans corrections majeures.**

---

## 1. PROBLÈMES DE CONCURRENCE

### 1.1 Race Condition : `c.Logs` dans `container.go` (CRITIQUE)

**Localisation:** `docker/container.go`, lignes 154-165, 171-175

```go
func (c *Container) AppendLog(line string) {
    if c.deleted.Load() {
        return
    }
    c.Logs = append(c.Logs, line)  // ÉCRITURE NON PROTÉGÉE
    if len(c.Logs) > maxLogLines {
        newLogs := make([]string, maxLogLines)
        copy(newLogs, c.Logs[len(c.Logs)-maxLogLines:])
        c.Logs = newLogs
    }
}
```

**Analyse:**
- `c.Logs` est accédé depuis plusieurs goroutines sans synchronisation
- Le commentaire dans `Delete()` qui dit "will be garbage collected" est une **justification insuffisante** d'une race condition
- **RISQUE:** Data corruption, crash, comportement imprévisible

---

### 1.2 Accès Non-Atomique à Plusieurs Atomics (Race Condition Subtile)

**Localisation:** `docker/docker.go`, lignes 1249-1260

```go
if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
    d.statsContextsLock.Lock()
    if oldCancel, exists := d.statsContexts[c.ID]; exists {
        oldCancel()
    }
    c.statsGen.Add(1)  // ATOMIC WRITE sous lock
    statsCtx, statsCancel := context.WithCancel(ctx)
    d.statsContexts[c.ID] = statsCancel
    d.statsContextsLock.Unlock()

    go func() {
        d.getContainerStatsRealtime(statsCtx, c)  // LIRE statsGen SANS MUTEX
    }()
}
```

**Problème:**
- `c.statsGen` est modifié avec le lock tenu
- Mais la nouvelle goroutine lit `statsGen` dans `getContainerStatsRealtime()` sans synchronisation
- **RISQUE:** Race condition subtile sur la lecture/écriture de `statsGen`

---

### 1.3 Fermeture de Channel Sans Synchronisation

**Localisation:** `tui/tui.go`, ligne 853

```go
func (t *Tui) cleanup() {
    t.closing.Store(true)
    // ...
    close(t.requestData)  // FERMETURE SANS GARANTIE
}
```

**Problème:**
- `t.requestData` est fermé sans garantir qu'aucune goroutine n'essaie d'envoyer dessus
- Si une goroutine appelle `t.requestData <- ...` pendant la fermeture → **panic "send on closed channel"**
- **RISQUE:** Crash intermittent en shutdown

---

### 1.4 Timer Leak Subtil dans `PausableRefresh.Pause()`

**Localisation:** `tui/sync.go`, lignes 106-128

```go
func (p *PausableRefresh) Pause(duration time.Duration, checkClosing func() bool) {
    p.paused.Store(true)
    p.mu.Lock()
    defer p.mu.Unlock()

    if p.timer != nil {
        if !p.timer.Stop() {
            select {
            case <-p.timer.C:
            default:
            }
        }
    }

    p.timer = time.AfterFunc(duration, func() {  // NOUVEAU TIMER
        if !checkClosing() {
            p.paused.Store(false)
        }
    })
}
```

**Problème:**
- Le `time.AfterFunc()` crée un timer qui n'est jamais explicitement arrêté si le contexte se termine
- **RISQUE:** Timer leak si `Pause()` est appelé juste après un arrêt

---

### 1.5 Map Concurrency : Itération Longue Bloquant Les Écritures

**Localisation:** `docker/docker.go`, lignes 558-560

```go
functor: func(docker *Docker) *Container {
    containers := make([]*Container, 0, len(docker.containers))
    for _, c := range docker.containers {  // ITÉRATION POTENTIELLEMENT LONGUE
        containers = append(containers, c)
    }
    containersChan <- containers
    return nil
},
```

**Problème:**
- L'itération se fait dans le functor qui bloque `handleContainersCommand()`
- Si beaucoup de containers, toutes les autres écritures sont bloquées
- **RISQUE:** Deadlock potentiel avec timeouts cumulés

---

### 1.6 Context Cancellation Race dans `demuxLogStream()`

**Localisation:** `docker/docker.go`, lignes 777-788

```go
func (d *Docker) demuxLogStream(...) error {
    defer pw.Close()
    defer closeResources()
    _, err := stdcopy.StdCopy(pw, pw, logStream)  // BLOQUANT SANS TIMEOUT
    // ...
}
```

**Problème:**
- `stdcopy.StdCopy()` est un appel bloquant sans timeout
- Si le Docker daemon se fige, cette goroutine restera bloquée indéfiniment
- **RISQUE:** Goroutine zombie

---

## 2. FUITES DE RESSOURCES

### 2.1 Goroutine Leak sur Timeout

**Localisation:** `docker/docker.go`, lignes 260-277

```go
response := make(chan *Container, 1)
timer1 := time.NewTimer(model.ChannelTimeout)

select {
case d.containersCommand <- ContainersCommand{...}:
    timer.Stop(timer1)
case <-timer1.C:
    r.Response <- model.Container{}
    return  // RETOUR SANS NETTOYER response CHANNEL
case <-ctx.Done():
    timer.Stop(timer1)
    r.Response <- model.Container{}
    return  // RETOUR SANS NETTOYER response CHANNEL
}
```

**Problème:**
- En cas de timeout, le channel `response` n'est jamais vidé
- Si le functor envoie ultérieurement dans `response`, il sera bloqué
- **RISQUE:** Accumulation de goroutines zombies

---

### 2.2 Erreurs d'I/O Ignorées

**Localisation:** `docker/docker.go`, ligne 730

```go
if out != nil {
    out.Close()  // ERREUR IGNORÉE
}
```

**Problème:**
- L'erreur de `out.Close()` est ignorée
- Pattern Go correct: `if err := out.Close(); err != nil { /* handle */ }`

---

### 2.3 Goroutine Leak dans `getContainerStatsRealtime()` sur Panics

**Localisation:** `docker/docker.go`, lignes 1080-1097

**Problème:**
- Il n'y a **aucun recover** dans `getContainerStatsRealtime()` pour arrêter la boucle en cas d'erreur catastrophique
- **RISQUE:** Goroutine qui continue indéfiniment même après un problème grave

---

## 3. PROBLÈMES D'I/O ET TIMEOUTS

### 3.1 Timeout Trop Agressif (5 Secondes)

**Localisation:** `dto/constants.go`, ligne 7

```go
const ChannelTimeout = 5 * time.Second
```

**Problème:**
- 5 secondes est un timeout **trop agressif** pour les opérations Docker
- Sur un système chargé, cela causera des timeouts légitimes
- **RISQUE:** Perte de données, déconnexions prématurées

**Recommandation:** Utiliser 10-30 secondes selon le contexte

---

### 3.2 Lectures Sans Timeout dans `getContainerStatsRealtime()`

**Localisation:** `docker/docker.go`, lignes 1051-1069

```go
for {
    var stats apiContainer.StatsResponse
    errDecode := dec.Decode(&stats)  // BLOQUANT SANS TIMEOUT
    // ...
}
```

**Problème:**
- L'appel `dec.Decode()` est bloquant sans timeout
- Si Docker daemon freeze, cette goroutine reste bloquée indéfiniment

---

## 4. MAUVAISES PRATIQUES GO

### 4.1 Erreurs Ignorées Systématiquement (Anti-pattern)

**Localisations multiples:**

a) `docker/docker.go`, ligne 730:
```go
out.Close()  // ERREUR IGNORÉE
```

b) `docker/docker.go`, ligne 746:
```go
logStream.Close()  // ERREUR IGNORÉE
pr.Close()         // ERREUR IGNORÉE
```

**Problème:** Erreurs I/O critiques ignorées silencieusement

---

### 4.2 Double Recover Anidé (Code Smell)

**Localisation:** `docker/docker.go`, lignes 1121-1143

```go
defer func() {
    if r := recover(); r != nil {
        // ...
    }
    if cmd.response != nil {
        func() {
            defer func() {
                if r := recover(); r != nil {  // DOUBLE RECOVER
                    // ...
                }
            }()
            cmd.response <- c
        }()
    }
}()
```

**Problème:**
- Double recover anidés indique un design fragile
- Les functors ne devraient jamais panicker

---

### 4.3 Context Orphelin avec `context.Background()`

**Localisation:** `tui/sync.go`, ligne 320

```go
func NewActionController(maxConcurrent int) *ActionController {
    ctx, cancel := context.WithCancel(context.Background())  // ORPHELIN
    // ...
}
```

**Problème:**
- Le context est orphelin et ne peut pas être annulé par le parent
- Le context devrait être passé en paramètre

---

### 4.4 Magic Numbers Partout

**Localisations multiples:**

- `docker/docker.go`, ligne 123: `16` (buffer size)
- `docker/docker.go`, ligne 122: `256` (map capacity)
- `tui/constants.go`, ligne 17: `10` (maxConcurrentActions)

**Problème:**
- Pas de constants documentées pour les nombres critiques
- Difficile de justifier les choix de tuning

---

### 4.5 Variable Shadowing

**Localisation:** `docker/docker.go`, ligne 1079

```go
s := stats  // Shadowing possible
```

**Problème:** Portée confuse, risque d'utiliser la mauvaise variable

---

### 4.6 JSON Unmarshalling Inefficace

**Localisation:** `tui/log_formatter.go`, ligne 120

```go
var logData map[string]any  // Devrait être un struct
```

**Problème:**
- Unmarshalling en `map[string]any` est moins efficace qu'en struct
- Type assertions partout

---

## 5. PROBLÈMES DE MÉMOIRE

### 5.1 Maps Qui Grossissent Sans Limite

**Localisation:** `docker/docker.go`, lignes 42, 49, 53

```go
type Docker struct {
    containers        map[model.ContainerID]*Container
    statsContexts     map[model.ContainerID]context.CancelFunc
    logContexts       map[model.ContainerID]context.CancelFunc
}
```

**Problème:**
- Les contexts ne sont pas toujours nettoyés proprement en cas d'erreur
- **RISQUE:** Memory leak sur longue durée

---

### 5.2 Slices Qui Retiennent des Références

**Localisation:** `docker/docker.go`, lignes 557-560

```go
containers := make([]*Container, 0, len(docker.containers))
for _, c := range docker.containers {
    containers = append(containers, c)  // POINTEURS
}
```

**Problème:**
- Le slice contient des pointeurs aux containers
- Si le container est supprimé mais que le slice est conservé, données en mémoire

---

### 5.3 Logs Sans Monitoring de Mémoire

**Localisation:** `docker/container.go`, lignes 154-165

**Problème:**
- 1000 lignes par container peut être insuffisant ou excessif
- Pas de monitoring de la taille mémoire utilisée par les logs
- **RISQUE:** OOM avec beaucoup de containers

---

## 6. PROBLÈMES DE SÉCURITÉ

### 6.1 Validation Non Uniforme des Container IDs

**Localisation:** `tui/actions.go`, lignes 24-35

```go
func isValidContainerID(id string) bool {
    // Validation présente mais...
}
```

**Problème:**
- La validation n'est **pas utilisée uniformément** partout
- `handleEvents()` ne valide pas les IDs

---

### 6.2 Données Sensibles en Logs

**Localisation:** `docker/docker.go`, ligne 94

```go
logger.Info("using Podman socket", "socket", podmanSocket)  // PATH EN CLAIR
```

**Problème:**
- Chemins de sockets et projets loggés en clair
- Peut contenir des informations sensibles

---

## 7. DESIGN ET ARCHITECTURE

### 7.1 Violation du Principe de Responsabilité Unique

**Localisation:** `docker/docker.go`

**Problème:**
- La classe `Docker` a trop de responsabilités:
  1. Gestion de la connexion Docker
  2. Gestion des containers
  3. Gestion des stats
  4. Gestion des logs
  5. Gestion des requêtes du TUI

---

### 7.2 Code Dupliqué: Pattern Request/Response Répété

**Localisations:**
- `handleRequestContainerLog()`
- `handleRequestContainerProject()`
- `handleRequestProjectList()`

**Problème:**
- Le même pattern copié/collé 5+ fois avec des variations mineures
- Devrait être factorisé

---

### 7.3 Interface Marker (Anti-pattern)

**Localisation:** `dto/request.go`, lignes 3-6

```go
type RequestData interface {
    isRequestData()  // MARKER METHOD
}
```

**Problème:**
- Marker interface sans intention claire
- Mieux: utiliser une interface avec des méthodes significatives

---

## 8. GESTION DES ERREURS

### 8.1 Erreurs Sentinelles Manquantes

**Problème:**
- Pas de `var ErrTimeout`, `var ErrContainerNotFound`, etc.
- Les erreurs sont juste des strings génériques
- Impossible d'utiliser `errors.Is()` proprement

---

### 8.2 Wrapping d'Erreurs Inconsistent

**Localisation:** `main.go`, ligne 45

```go
return fmt.Errorf("failed to initialize docker client: %w", err)
```

vs. autres endroits qui ne wrappent pas avec `%w`.

---

### 8.3 Context Deadline Non Distingué

**Localisation:** `docker/docker.go`, lignes 1063-1067

**Problème:**
- Les timeouts sont traités comme des erreurs normales
- Devrait tenter une reconnexion

---

## 9. AUTRES PROBLÈMES

### 9.1 Pas de `sync.Pool` pour les Channels

**Problème:**
- Chaque requête crée de nouveaux channels
- Pour une application haute performance, considérer `sync.Pool`

---

### 9.2 Pas de Healthcheck Docker

**Problème:**
- Aucun healthcheck pour vérifier si Docker/Podman est accessible
- Si le daemon crash, erreurs silencieuses

---

### 9.3 Pas de Metrics ou Instrumentation

**Problème:**
- Aucune métrique sur le nombre de goroutines, consommation mémoire, latence, taux d'erreurs

---

## 10. RÉSUMÉ DES PROBLÈMES CRITIQUES

| # | Problème | Sévérité | Impact |
|---|----------|----------|--------|
| 1.1 | Race condition sur `c.Logs` | CRITIQUE | Data corruption, crash |
| 1.2 | Accès non-atomic à `statsGen` | HAUTE | Race condition subtile |
| 1.3 | Channel closing race | HAUTE | Panic en shutdown |
| 1.5 | Map concurrency | HAUTE | Deadlock potentiel |
| 2.1 | Goroutine leaks sur timeout | HAUTE | Accumulation goroutines |
| 3.1 | Timeout trop agressif | HAUTE | Perte de données |
| 4.1 | Erreurs I/O ignorées | MOYENNE | Debugging difficile |
| 5.1 | Maps sans limite | MOYENNE | Memory leak |
| 5.3 | Logs sans monitoring | MOYENNE | OOM possible |
| 6.1 | Validation non uniforme | MOYENNE | Sécurité |

---

## 11. RECOMMANDATIONS PRIORITAIRES

### Priorité 1 (Critique - Blocker)
1. **Refactorer la synchronisation de `c.Logs`** - utiliser un vrai RWMutex
2. **Implémenter des tests avec race detector** (`go test -race`)
3. **Augmenter ChannelTimeout** à 10-30s selon le contexte

### Priorité 2 (Haute)
4. Nettoyer les goroutines orphelines en cas de timeout
5. Implémenter une gestion d'erreurs cohérente avec sentinelles
6. Protéger la fermeture de `requestData` channel

### Priorité 3 (Moyenne)
7. Refactorer le pattern request/response pour éliminer la duplication
8. Ajouter des healthchecks et metrics
9. Valider tous les inputs uniformément
10. Remplacer les magic numbers par des constantes documentées

---

## CONCLUSION

Le projet a une architecture globale solide. Cependant, il souffre de **problèmes de concurrence importants** qui pourraient causer des crashes, data corruptions, et leaks en production.

**Note Globale: 4/10**

Une revue et refactorisation des aspects de concurrence est **OBLIGATOIRE** avant mise en production.

---

*Rapport généré par Claude Code Review - Mode Acerbe*
