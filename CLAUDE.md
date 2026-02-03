# Commands
- audit-branches: scripts/manage_branches.sh          # Audit local branches
- clean-branches: scripts/manage_branches.sh --delete # Interactive branch cleanup
- force-clean-branches: scripts/manage_branches.sh --delete --force # Silent cleanup (Agent Safe)
- audit-commits: scripts/audit_commit_authors.sh      # Audit commit identities
- build: make build                                   # Build binaries
- lint: make lint                                     # Run linters
- test: make test                                     # Run tests
