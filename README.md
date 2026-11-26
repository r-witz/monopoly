# Quartiers (nom de travail)

Jeu de plateau multijoueur dans le navigateur : on achète des rues, on encaisse des loyers, on
construit, on négocie, en version « classique rapide ». Ce dépôt contient **le serveur Go, le
moteur de règles** et **le client 3D** (`web/`), plus un client de test brut (`web/test/`).

Le plateau, les noms, les prix et les cartes sont propres au jeu : 40 cases (4 coins et 4 côtés de 9,
chaque groupe sur un seul côté), une ville portuaire fictive, 8 groupes de rues, 4 lignes de
transport, 2 compagnies et un paquet de cartes « Rumeur ».

## Lancer

```sh
go run .                  # http://localhost:3000 : le jeu ; /test/ : le client de test ; ws://localhost:3000/ws, /healthz
go test ./...             # moteur : 3000 parties aléatoires + cas ciblés ; serveur : parties de bots en WebSocket
go test -race ./internal/server
```

| Variable | Défaut | Rôle |
| --- | --- | --- |
| `PORT`, `HOST` | `3000`, `0.0.0.0` | adresse d'écoute |
| `MAX_ROOMS` | `200` | salles ouvertes en même temps |
| `TRUST_PROXY` | – | `1` derrière **un** reverse proxy : l'IP vient de `X-Forwarded-For` |

**Tester seul en local** : ouvrir http://localhost:3000 dans un onglet, créer une salle, puis
ouvrir le lien de la salle (`/#CODE`) dans d'autres onglets. Chaque onglet est un joueur, car le
jeton et le nom sont gardés par onglet. Les clients sont du HTML et du JS sans compilation,
embarqués dans le binaire : il faut relancer `go run .` après une modification.

## Docker

Image `scratch` : rien d'autre que le binaire statique, client compris (11 Mo selon
`docker images`), utilisateur non root, aucun fichier écrit. Elle tourne telle quelle en lecture
seule et sans capacités :

```sh
docker build -t quartiers . && docker run --read-only --cap-drop ALL --security-opt no-new-privileges -p 3000:3000 quartiers
```

- la construction lance les tests (`go test -short ./...`) et s'arrête s'ils échouent ;
- le binaire est compilé sans HTTP/2 (`-tags nethttpomithttp2`) : le serveur ne parle que
  HTTP/1.1 en clair, le TLS est l'affaire du reverse proxy ;
- le healthcheck est le binaire lui-même (`quartiers healthcheck`), qui vérifie seulement que le
  port accepte une connexion ;
- les caches de Go sont gardés entre deux constructions (BuildKit) : seul ce qui a changé est
  recompilé ;
- il faut Docker 25 ou plus récent (`--start-interval`).

**En production**, `compose.yaml` lance le jeu seul, sur le port 3000 du serveur. Les réglages
facultatifs vont dans un fichier `.env` à côté : `PORT`, `MAX_ROOMS`, et `TRUST_PROXY=1`
seulement derrière un reverse proxy. Pour lancer comme pour mettre à jour :
`git pull && docker compose up -d --build`. Si la construction échoue (un test cassé), l'ancien
conteneur continue de tourner. Pour HTTPS, il faut un reverse proxy devant, qui laisse passer les
WebSockets de `/ws`.

Tout est en mémoire : un redémarrage perd les salles. Les clients reçoivent la fermeture 1012 et
se reconnectent.

## Ce qu'il y a dedans

```
main.go              serveur HTTP : /ws, /healthz, le client de test (/)
web/                 le client 3D, sans compilation (modules ES)
  scene.js           la 3D (Three.js r170 embarqué dans web/vendor) : île, plateau, cases dessinées
                     depuis le plateau du serveur, pions, dés, maisons, caméra, clic sur une case
  hud.js             l'interface HTML par-dessus : accueil, salon, joueurs, actions, fiche de case,
                     échanges, journal, carte piochée, podium
  main.js            connexion, état, clics ; joue les événements d'un message « game » l'un après
                     l'autre (dés, pion case par case, carte…), puis affiche l'état
  vendor/            Three.js (MIT), polices DynaPuff et Nunito (OFL), reprises du golf
web/test/            client de test brut : index.html + app.js
internal/engine      les règles, une machine à états pure : ni horloge, ni réseau, ni verrou, aléa à graine
  board.go           le plateau et les cartes (données)
  game.go            état, phases, Apply / Timeout / Resign / Finish, classement
  turn.go            dés, déplacement, cases, cartes, prison, dettes
  assets.go          constructions, revente à la banque, liquidation, faillite
  market.go          achats, échanges
  view.go            ce que reçoivent les clients (état complet + événements)
internal/server      salles et connexions, repris de P'tits Trous (../golf) : hub, room, conn, shard
  game.go            la partie dans la salle : démarrage, minuteurs, diffusion
```

Le socle réseau est celui du golf, presque à l'identique : sessions par jeton (reconnexion
transparente), salles par code ou lien, publiques ou privées, propriétaire qui règle, lance,
arrête et exclut, anti-flood, écriture non bloquante. Le protocole est décrit en tête de
`internal/server/proto.go`, et les actions de jeu en tête de `engine.Game.Apply`.

## Les règles (classique rapide)

- 2 à 8 joueurs. Chacun commence avec de l'argent seulement, sans aucune propriété. Le
  propriétaire de la salle règle l'argent de départ, le salaire, la fin de partie et les minuteurs.
- **Fin** : au bout de N tours de table (par défaut 25), au bout de N minutes, ou au dernier
  survivant. Dans les deux premiers cas, le plus riche gagne (cash + propriétés + constructions).
- **Victoire immédiate** (chacune activable dans les paramètres) : posséder les 4 lignes de
  transport, 3 quartiers complets (ces deux-là actives par défaut), ou tous les quartiers d'un
  même côté du plateau (désactivée par défaut : ses 2 quartiers coûtent peu) termine la partie
  aussitôt. Vérifié à chaque achat, échange accepté ou faillite qui
  donne des propriétés à un créancier. Un joueur endetté ne gagne qu'après avoir payé sa dette.
- On lance 2 dés. Un double fait rejouer, trois doubles envoient au poste de police. Pour sortir :
  faire un double, payer 50 ou jouer une carte. Après 3 essais, l'amende est due de toute façon.
- **Case libre** : on l'achète, ou on la laisse : elle reste à vendre, sans enchère.
- **Maisons** : en achetant une rue, on la prend nue ou avec 1 ou 2 maisons (3 une fois passé
  le Départ). Ensuite, on ajoute des maisons chaque fois qu'on retombe sur sa rue, une fois par
  passage, sans avoir besoin du groupe complet. La tour se construit sur une rue à 3 maisons, en y
  retombant. Une rue bâtie ne s'échange pas. Il n'y a pas d'hypothèque.
- **Aéroport** (le coin opposé au Départ) : en s'y arrêtant, au tour suivant, au lieu de lancer
  les dés, on peut s'envoler vers la case de son choix pour 50, sauf le Départ. On y joue comme si
  on y était arrivé aux dés. Un vol ne passe pas par le Départ : ni salaire, ni tour du plateau.
  Lancer les dés renonce au vol.
- **Départ pile** : en tombant pile sur le Départ (aux dés ou par une carte), pile ou face : un
  lancer de plus (après un éventuel double), ou construire sur la rue de son choix, comme si on y
  retombait.
- **Loyers** : quand on a tout un groupe, ses rues sans maison rapportent le double, ses rues
  bâties (maisons ou tour) une fois et demie, arrondi au-dessus. Les rues reprennent Business
  Tour, en milliers (on démarre avec 2 000, ses 2 millions) : prix, maisons, et une tour plus
  chère qu'une maison (150 à 500, comme son hôtel). Les loyers viennent des relevés d'un joueur.
  Lignes, compagnies, taxes et amende restent celles du jeu classique.
- **Fin du tour** : elle se fait toute seule dès qu'il n'y a plus rien à décider. Le joueur ne la
  donne à la main que s'il peut encore construire (sur sa rue, ou au Départ pile) et a de quoi
  payer au moins un niveau : les échanges se font de toute façon à tout moment.
- **Dette** : quand on doit plus que son argent, une fenêtre s'ouvre pour revendre à la banque les
  propriétés de son choix, au prix payé, maisons et tour comprises. Elles redeviennent libres, et
  la dette est payée dès que l'argent la couvre. On peut aussi échanger, sinon c'est la faillite :
  les constructions sont rachetées par la banque à leur coût, et tout part au créancier, ou
  retourne à la banque. On ne vend jamais rien hors d'une dette.
- **Échanges** : propriétés et argent dans les deux sens, à tout moment. Une offre est revérifiée
  à l'acceptation, et un nouvel envoi remplace l'offre précédente du même joueur.
- **Minuteur** : si le joueur ne décide pas à temps, le serveur joue à sa place. Il lance les dés,
  refuse l'achat, revend des propriétés pour payer une dette (celles dont le total couvre la
  dette au plus juste, pour garder les plus belles ; ou déclare la faillite), puis finit le tour. Une partie ne bloque donc jamais.

Choix faits pour la salle :

- qui arrive en cours de partie la regarde, sans jouer ;
- quitter la salle ou en être exclu pendant la partie vaut abandon : les biens retournent à la
  banque ;
- un joueur déconnecté garde sa place 5 minutes, le minuteur jouant pour lui. Au-delà, il
  abandonne.

Limite connue : un joueur endetté peut encore céder ses propriétés par échange avant de faire
faillite. Il ne peut pas céder d'argent.
