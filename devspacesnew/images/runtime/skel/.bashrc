# ~/.bashrc — seeded into your persistent home on first boot; edit freely.
case $- in *i*) ;; *) return ;; esac

# Tool environment installed by image features (also used by VS Code terminals).
for f in /etc/profile.d/workspace-*.sh; do [ -r "$f" ] && . "$f"; done
export PATH="$HOME/.local/bin:$PATH"

# Persistent, append-only history: flushed after every command so nothing is
# lost when the pod is stopped, merged across tmux panes and sessions.
HISTFILE="$HOME/.bash_history"
HISTSIZE=50000
HISTFILESIZE=100000
HISTCONTROL=ignoredups
shopt -s histappend checkwinsize
PROMPT_COMMAND='history -a; history -n'

[ -r /usr/share/bash-completion/bash_completion ] && . /usr/share/bash-completion/bash_completion
PS1='\[\e[1;32m\]${WORKSPACE_NAME:-ws}\[\e[0m\]:\[\e[1;34m\]\w\[\e[0m\]\$ '
alias ll='ls -alF'
alias ws-logs='sudo tail -n 50 /var/log/workspace/*.log'
