# Revue de Code Critique - c8s

## Verdict Global: Code Fonctionnel mais Architectural Discutable

Le projet fonctionne, mais souffre de nombreux problèmes de conception et de maintenabilité qui le rendraient difficile à faire évoluer dans un contexte professionnel.

---

## 1. PROBLÈMES ARCHITECTURAUX GRAVES

### 1.1 Le Pattern "Functor" - Un Anti-Pattern Déguisé

**Fichiers concernés:** `docker/docker.go:27-30`, `docker/container.go:43-46`

```go
type ContainersCommand struct {
    functor  func(*Docker) *Container
    response chan *Container
}
```

**Critique acerbe:**
- Ce pattern est une **mauvaise abstraction**. Vous passez des closures au lieu de définir des commandes typées explicitement.
- Ironiquement, le fichier `docker/commands.go` définit exactement ce qu'il faudrait utiliser (commandes typées avec interface), mais **il n'est jamais utilisé**! C'est du code mort.
- Ce design obscurcit la logique, rend le debugging difficile, et empêche le compilateur de vérifier l'exhaustivité des cas.

### 1.2 Couplage Fort Entre Packages

**Fichier:** `docker/docker.go:24`

```go
import "github.com/syrm/c8s/tui"
```

Le package `docker` importe `tui` pour utiliser `tui.RequestData`. C'est une **violation flagrante de la séparation des préoccupations**. Le layer Docker ne devrait JAMAIS connaître l'existence du TUI.

**Solution attendue:** Définir les interfaces de communication dans un package `domain` ou directement dans `dto`.

### 1.3 La Struct `Tui` - Le God Object

**Fichier:** `tui/tui.go:53-110`

Cette struct contient **57 champs**. C'est un **God Object** classique qui viole le principe de responsabilité unique. Elle gère:
- L'état de 3 vues différentes
- Le tri et filtrage
- Les timers
- Les locks de synchronisation
- Les données de cache
- La navigation

---

## 2. GESTION DE LA CONCURRENCE - CAUCHEMAR EN PUISSANCE

### 2.1 Prolifération de Locks

**Fichier:** `tui/tui.go`

```go
tableProjectDataLock       sync.RWMutex
tableContainerDataLock     sync.RWMutex
logPausedLock              sync.RWMutex
logShowTimestampLock       sync.RWMutex
logFilterLock              sync.RWMutex
currentViewLock            sync.RWMutex
containerRefreshPausedLock sync.RWMutex
containerRefreshTimerLock  sync.Mutex
projectRefreshPausedLock   sync.RWMutex
projectRefreshTimerLock    sync.Mutex
containerDisappearedLock   sync.RWMutex
```

**11 locks différents!** C'est une recette pour:
- Deadlocks potentiels (l'ordre d'acquisition n'est pas documenté)
- Performance dégradée
- Code impossible à raisonner

### 2.2 Pattern Lock/Unlock Manuel Dangereux

**Fichier:** `tui/setup.go:131-139`

```go
t.projectRefreshPausedLock.Lock()
t.projectRefreshPaused = false
t.projectRefreshPausedLock.Unlock()
t.projectRefreshTimerLock.Lock()
if t.projectRefreshTimer != nil {
    t.projectRefreshTimer.Stop()
    t.projectRefreshTimer = nil
}
t.projectRefreshTimerLock.Unlock()
```

Répété partout au lieu d'utiliser `defer`. Si une panique survient entre Lock et Unlock, le système est bloqué définitivement.

### 2.3 Race Condition Évidente

**Fichier:** `tui/tui.go:373-377`

```go
t.tableContainerDataLock.RLock()
containers := slices.SortedStableFunc(maps.Values(t.tableContainerData), ...)
t.tableContainerDataLock.RUnlock()
// ... puis utilisation de containers sans lock
```

Le lock est relâché AVANT l'utilisation des données. Si un autre goroutine modifie la map entre temps, comportement indéfini.

---

## 3. GESTION DES ERREURS - QUASI INEXISTANTE

### 3.1 Erreurs Silencieusement Ignorées

**Fichier:** `tui/actions.go:87, 106, 137, 156`

```go
_ = cmd.Run()  // Répété 4 fois
```

Les erreurs d'exécution Docker sont **complètement ignorées**. L'utilisateur n'a aucun feedback si `docker stop`, `docker start`, `docker rm` échouent.

### 3.2 Panic Utilisé Comme Gestion d'Erreur

**Fichier:** `main.go:17-18`

```go
if err != nil {
    panic(err)
}
```

Un fichier de log inaccessible fait crasher toute l'application. Une gestion gracieuse serait préférable.

### 3.3 os.Exit au Milieu du Code

**Fichiers:** `docker/docker.go:49`, `tui/tui.go:881`

```go
os.Exit(1)
```

Appelé directement dans les packages, court-circuitant tout cleanup potentiel. Les `defer` ne seront pas exécutés.

---

## 4. CODE MORT ET DUPLICATION

### 4.1 Fichier Entièrement Inutilisé

**Fichier:** `docker/commands.go`

45 lignes de code définissant des commandes typées... **jamais utilisées**. Ce fichier devrait soit être supprimé, soit être intégré à la place du pattern functor.

### 4.2 Fonction Non Utilisée

**Fichier:** `docker/container.go:149-170`

```go
func isRunningFromAction(action events.Action) (bool, error)
```

Fonction définie mais jamais appelée nulle part dans le codebase.

### 4.3 Interface Non Implémentée Utilement

**Fichier:** `dto/container.go:7-9`

```go
type ContainerDeletable interface {
    Deleted() bool
}
```

`ContainerDeleted` implémente cette interface mais n'est jamais utilisé. `Container.Deleted()` retourne toujours `false`.

### 4.4 Duplication Massive

**Fichiers:** `tui/setup.go`, `tui/tui.go`

Le code pour:
- Acquérir un lock
- Modifier un état
- Libérer le lock
- Mettre à jour un timer

Est répété textuellement dans `pauseContainerRefresh()`, `pauseProjectRefresh()`, `exitContainerView()`, `exitLogView()`, etc.

---

## 5. PROBLÈMES DE SÉCURITÉ

### 5.1 Injection de Commande Potentielle

**Fichier:** `tui/actions.go:83`

```go
cmd := exec.Command("docker", "exec", "-it", string(container.ID), "/bin/sh")
```

Le `container.ID` vient des labels Docker. Si un attaquant contrôle les labels, injection possible. Même si peu probable, le pattern est dangereux.

### 5.2 Permissions de Fichier Trop Permissives

**Fichier:** `main.go:15`

```go
os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
```

Permission `0666` = lecture/écriture pour TOUS les utilisateurs. Devrait être `0600` minimum.

---

## 6. PROBLÈMES DE DESIGN API

### 6.1 Context Passé Puis Ignoré

**Fichier:** `docker/container.go:48-54`

```go
func NewContainer(
    ctx context.Context,  // Passé...
    ...
) *Container {
    ctx, cancel := context.WithCancel(ctx)  // ... puis immédiatement écrasé
```

Le context parent est systématiquement écrasé, rendant impossible l'annulation depuis l'extérieur.

### 6.2 Channels Non Fermés

Les nombreux channels créés (`Command`, `containersCommand`, `requestData`) ne sont jamais fermés explicitement. Le garbage collector devrait s'en occuper, mais c'est une mauvaise pratique qui peut mener à des goroutines zombies.

### 6.3 Valeurs Magiques

**Fichier:** `docker/docker.go:53`

```go
containers: make(map[ContainerID]*Container, 256)
```

Pourquoi 256? Pas documenté. Allocation prématurée arbitraire.

---

## 7. STYLE ET LISIBILITÉ

### 7.1 Nommage Incohérent

- `tableProject` vs `tableContainer` vs `tableContainerLog` (table vs log)
- `currentView` (type) vs `currentView` (champ) - shadowing
- `RequestProjectList` vs `RequestProject` vs `RequestContainerLog` - nomenclature incohérente

### 7.2 Fonctions Trop Longues

**Fichier:** `tui/tui.go:611-873`

`getData()` fait **262 lignes**. C'est illisible. Devrait être découpé en sous-fonctions.

### 7.3 Commentaires Inutiles

```go
// Create tables
tableProject := createTable()
```

Le commentaire n'apporte aucune valeur.

---

## 8. TESTS - INEXISTANTS

Selon le `CLAUDE.md`: "*This project doesn't need tests*"

C'est **inacceptable** pour du code de production. Le code est complexe avec:
- Concurrence
- État mutable partagé
- Intégration avec API externe (Docker)

Sans tests, aucune garantie de non-régression.

---

## 9. PROBLÈMES MINEURS MAIS NOMBREUX

| Fichier | Ligne | Problème |
|---------|-------|----------|
| `tui/tui.go` | 326 | `offset` initialisé mais jamais modifié |
| `tui/sorting.go` | 23 | Conversion `rune(textLower[textIdx])` incorrecte pour Unicode |
| `docker/docker.go` | 273-275 | Comparaison `err != io.EOF` après que err soit assigné |
| `tui/log_formatter.go` | 100-108 | Liste de formats timestamp dupliquée avec ligne 157-165 |
| `dto/project.go` | 11-13 | Maps dans struct = sharing implicite dangereux |

---

## RÉSUMÉ

| Catégorie | Sévérité | Count |
|-----------|----------|-------|
| Architecture | CRITIQUE | 3 |
| Concurrence | CRITIQUE | 3 |
| Gestion d'erreurs | MAJEUR | 3 |
| Code mort | MODÉRÉ | 4 |
| Sécurité | MODÉRÉ | 2 |
| Design API | MODÉRÉ | 3 |
| Style | MINEUR | 3+ |
| Tests | CRITIQUE | 1 |

**Note globale: 4/10**

Le code fonctionne pour un usage personnel, mais ne passerait pas une revue de code dans une équipe professionnelle. Les problèmes de concurrence et l'absence de tests sont particulièrement préoccupants.

---

## PRIORITÉS DE REFACTORING

1. **Refactoriser le God Object `Tui`** en plusieurs composants spécialisés
2. **Supprimer ou utiliser `docker/commands.go`** - le code mort est inacceptable
3. **Revoir la gestion de la concurrence** - trop de locks manuels
4. **Ajouter une gestion d'erreur** pour les commandes Docker
5. **Découpler `docker` de `tui`** - inverser la dépendance
