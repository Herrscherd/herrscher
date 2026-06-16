# Herrscher

Repo parapluie du produit Herrscher : ne contient que des symlinks vers les
repos qui le composent.

- `core/`      → herrscher-core (la plateforme, tourne seule)
- `contracts/` → herrscher-contracts (le contrat de plugin — intégré par défaut)
- `discord/`   → herrscher-discord-gateway (adaptateur Discord — optionnel)

`dctl` n'est pas dans la famille : c'est une dépendance externe que le gateway
Discord consomme.
