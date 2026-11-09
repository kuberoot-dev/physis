# Shared helpers for the screencasts: comment lines in cyan, commands in yellow.
say() { printf '\n\033[1;36m# %s\033[0m\n' "$*"; sleep 1.5; }
show() { printf '\033[1;33m$ %s\033[0m\n' "$*"; sleep 0.6; eval "$@"; sleep 2.5; }
D=${D:-out/workshop}
k() { kubectl --kubeconfig $D/cluster.kubeconfig "$@"; }
