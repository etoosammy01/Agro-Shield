#!/usr/bin/env bash

set -e

# ============================================================
# AI CODING SETUP
# ============================================================
#
# Providers through AgentRouter:
#
#   Anthropic
#     - claude-opus-4-8
#     - claude-opus-5
#
#   DeepSeek
#     - deepseek-v4-flash
#
#   Zhipu AI
#     - glm-5.3
#
#   OpenAI
#     - gpt-5.6-sol
#
# Everything is optional.
#
# ============================================================

clear

echo "============================================================"
echo "              AI CODING + AGENTROUTER SETUP"
echo "============================================================"
echo
echo "All components are optional."
echo

# ============================================================
# HELPERS
# ============================================================

error_exit() {
    echo
    echo "============================================================"
    echo "ERROR: $1"
    echo "============================================================"
    echo
    exit 1
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

remove_whitespace() {
    printf '%s' "$1" | tr -d '[:space:]'
}

ask_yes_no() {
    local QUESTION="$1"

    while true; do
        read -r -p "$QUESTION [y/n]: " ANSWER

        case "$ANSWER" in
            y|Y|yes|YES)
                return 0
                ;;
            n|N|no|NO)
                return 1
                ;;
            *)
                echo "Please enter y or n."
                ;;
        esac
    done
}

# ============================================================
# OPERATING SYSTEM
# ============================================================

OS="$(uname -s)"

case "$OS" in
    Darwin)
        SHELL_CONFIG="$HOME/.zshrc"
        ;;

    Linux)
        if [ -f "$HOME/.zshrc" ]; then
            SHELL_CONFIG="$HOME/.zshrc"
        elif [ -f "$HOME/.bashrc" ]; then
            SHELL_CONFIG="$HOME/.bashrc"
        else
            SHELL_CONFIG="$HOME/.bashrc"
        fi
        ;;

    *)
        error_exit "Unsupported operating system: $OS"
        ;;
esac

echo "Operating system: $OS"
echo "Shell config:     $SHELL_CONFIG"
echo

# ============================================================
# CURL
# ============================================================

if ! command_exists curl; then
    echo "curl is not installed."

    if ! ask_yes_no "Continue anyway?"; then
        error_exit "Please install curl and run the script again."
    fi
fi

# ============================================================
# NVM
# ============================================================

echo
echo "============================================================"
echo "NVM"
echo "============================================================"
echo

if [ -s "$HOME/.nvm/nvm.sh" ]; then

    echo "NVM is already installed."

else

    if ask_yes_no "Install NVM?"; then

        if ! command_exists curl; then
            error_exit "curl is required to install NVM."
        fi

        echo
        echo "Installing NVM..."

        curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.6/install.sh | bash \
            || error_exit "NVM installation failed."

        echo
        echo "NVM installation completed."

    else

        echo "Skipping NVM."

    fi

fi

# ============================================================
# LOAD NVM
# ============================================================

export NVM_DIR="$HOME/.nvm"

if [ -s "$NVM_DIR/nvm.sh" ]; then
    # shellcheck disable=SC1090
    source "$NVM_DIR/nvm.sh"
fi

# ============================================================
# NODE.JS
# ============================================================

echo
echo "============================================================"
echo "Node.js"
echo "============================================================"
echo

if command_exists node; then

    echo "Node.js is already installed:"
    node -v

else

    if command_exists nvm; then

        if ask_yes_no "Install Node.js 22?"; then

            nvm install 22 \
                || error_exit "Node.js 22 installation failed."

            nvm use 22 \
                || error_exit "Could not switch to Node.js 22."

            nvm alias default 22 >/dev/null 2>&1 || true

        else

            echo "Skipping Node.js."

        fi

    else

        echo "NVM is not available."
        echo "Skipping Node.js."

    fi

fi

# ============================================================
# RELOAD NVM
# ============================================================

if [ -s "$NVM_DIR/nvm.sh" ]; then
    # shellcheck disable=SC1090
    source "$NVM_DIR/nvm.sh"
fi

echo

if command_exists node; then
    echo "Node.js: $(node -v)"
fi

if command_exists npm; then
    echo "npm:     $(npm -v)"
fi

# ============================================================
# AI MODEL SELECTION
# ============================================================

echo
echo "============================================================"
echo "                    AI MODEL SELECTION"
echo "============================================================"
echo
echo "Select the AI model you want to use."
echo
echo "Anthropic:"
echo "  1) claude-opus-4-8"
echo "  2) claude-opus-5"
echo
echo "DeepSeek:"
echo "  3) deepseek-v4-flash"
echo
echo "Zhipu AI:"
echo "  4) glm-5.3"
echo
echo "OpenAI:"
echo "  5) gpt-5.6-sol"
echo
echo "  6) Skip AI configuration"
echo

while true; do

    read -r -p "Choose [1-6]: " MODEL_CHOICE

    case "$MODEL_CHOICE" in

        1)
            PROVIDER="Anthropic"
            MODEL="claude-opus-4-8"
            break
            ;;

        2)
            PROVIDER="Anthropic"
            MODEL="claude-opus-5"
            break
            ;;

        3)
            PROVIDER="DeepSeek"
            MODEL="deepseek-v4-flash"
            break
            ;;

        4)
            PROVIDER="Zhipu AI"
            MODEL="glm-5.3"
            break
            ;;

        5)
            PROVIDER="OpenAI"
            MODEL="gpt-5.6-sol"
            break
            ;;

        6)
            PROVIDER=""
            MODEL=""
            break
            ;;

        *)
            echo
            echo "Invalid choice. Please choose 1-6."
            echo
            ;;

    esac

done

# ============================================================
# SKIP AI
# ============================================================

if [ -z "$MODEL" ]; then

    echo
    echo "============================================================"
    echo "AI CONFIGURATION SKIPPED"
    echo "============================================================"
    echo
    echo "No AI model was selected."
    echo
    echo "You can run this script again whenever you want."
    echo
    exit 0

fi

# ============================================================
# SELECTED MODEL
# ============================================================

echo
echo "============================================================"
echo "                    SELECTED AI"
echo "============================================================"
echo
echo "Provider: $PROVIDER"
echo "Model:    $MODEL"
echo

# ============================================================
# AGENTROUTER
# ============================================================

if ! ask_yes_no "Configure $MODEL through AgentRouter?"; then

    echo
    echo "AgentRouter configuration skipped."
    echo
    exit 0

fi

# ============================================================
# API KEY
# ============================================================

echo
echo "============================================================"
echo "                AGENTROUTER API KEY"
echo "============================================================"
echo
echo "Paste your AgentRouter API key."
echo "Your key will not be displayed."
echo "Whitespace will automatically be removed."
echo

while true; do

    read -r -s -p "AgentRouter API key: " AGENTROUTER_KEY
    echo

    AGENTROUTER_KEY="$(remove_whitespace "$AGENTROUTER_KEY")"

    if [ -n "$AGENTROUTER_KEY" ]; then
        break
    fi

    echo
    echo "API key cannot be empty."
    echo

done

# ============================================================
# SAVE GENERIC AGENTROUTER ENVIRONMENT
# ============================================================

echo
echo "============================================================"
echo "Saving AgentRouter configuration..."
echo "============================================================"
echo

TMP_CONFIG="$(mktemp)"

if [ -f "$SHELL_CONFIG" ]; then

    awk '
    BEGIN { skip=0 }

    /^# >>> AGENTROUTER AI CONFIGURATION >>>$/ {
        skip=1
        next
    }

    /^# <<< AGENTROUTER AI CONFIGURATION <<<$ / {
        skip=0
        next
    }

    skip == 0 {
        print
    }
    ' "$SHELL_CONFIG" > "$TMP_CONFIG"

else

    touch "$TMP_CONFIG"

fi

mv "$TMP_CONFIG" "$SHELL_CONFIG"

cat >> "$SHELL_CONFIG" <<EOF

# >>> AGENTROUTER AI CONFIGURATION >>>
export AGENTROUTER_API_KEY="$AGENTROUTER_KEY"
export AGENTROUTER_MODEL="$MODEL"
export AGENTROUTER_BASE_URL="https://agentrouter.org/v1"
# <<< AGENTROUTER AI CONFIGURATION <<<
EOF

export AGENTROUTER_API_KEY="$AGENTROUTER_KEY"
export AGENTROUTER_MODEL="$MODEL"
export AGENTROUTER_BASE_URL="https://agentrouter.org/v1"

# ============================================================
# OPTIONAL CLAUDE CODE
# ============================================================

if [ "$PROVIDER" = "Anthropic" ]; then

    echo
    echo "============================================================"
    echo "Claude Code"
    echo "============================================================"
    echo

    if ask_yes_no "Install/configure Claude Code?"; then

        if ! command_exists npm; then
            echo
            echo "npm is not available."
            echo "Claude Code installation skipped."
        else

            if command_exists claude; then

                echo "Claude Code is already installed."
                claude --version || true

            else

                echo "Installing Claude Code..."

                npm install -g @anthropic-ai/claude-code@latest \
                    || error_exit "Claude Code installation failed."

            fi

            # ------------------------------------------------
            # Claude environment
            # ------------------------------------------------

            TMP_CONFIG="$(mktemp)"

            awk '
            BEGIN { skip=0 }

            /^# >>> CLAUDE AGENTROUTER CONFIGURATION >>>$/ {
                skip=1
                next
            }

            /^# <<< CLAUDE AGENTROUTER CONFIGURATION <<<$ / {
                skip=0
                next
            }

            skip == 0 {
                print
            }
            ' "$SHELL_CONFIG" > "$TMP_CONFIG"

            mv "$TMP_CONFIG" "$SHELL_CONFIG"

            cat >> "$SHELL_CONFIG" <<EOF

# >>> CLAUDE AGENTROUTER CONFIGURATION >>>
export ANTHROPIC_AUTH_TOKEN="$AGENTROUTER_KEY"
export ANTHROPIC_BASE_URL="https://agentrouter.org"
export ANTHROPIC_MODEL="$MODEL"
# <<< CLAUDE AGENTROUTER CONFIGURATION <<<
EOF

            export ANTHROPIC_AUTH_TOKEN="$AGENTROUTER_KEY"
            export ANTHROPIC_BASE_URL="https://agentrouter.org"
            export ANTHROPIC_MODEL="$MODEL"

            echo
            echo "Claude Code configured."
            echo "Model: $MODEL"

        fi

    else

        echo "Claude Code skipped."

    fi

fi

# ============================================================
# OPTIONAL CODEX
# ============================================================

if [ "$PROVIDER" = "OpenAI" ]; then

    echo
    echo "============================================================"
    echo "Codex"
    echo "============================================================"
    echo

    if ask_yes_no "Install/configure Codex?"; then

        if ! command_exists npm; then

            echo
            echo "npm is not available."
            echo "Codex installation skipped."

        else

            if command_exists codex; then

                echo "Codex is already installed."
                codex --version || true

            else

                echo "Installing Codex..."

                npm install -g @openai/codex@latest \
                    || error_exit "Codex installation failed."

            fi

            # ------------------------------------------------
            # Codex configuration
            # ------------------------------------------------

            CODEX_DIR="$HOME/.codex"
            CODEX_CONFIG="$CODEX_DIR/config.toml"

            mkdir -p "$CODEX_DIR"

            if [ -f "$CODEX_CONFIG" ]; then

                BACKUP_FILE="$CODEX_CONFIG.backup.$(date +%Y%m%d_%H%M%S)"

                cp "$CODEX_CONFIG" "$BACKUP_FILE"

                echo
                echo "Existing Codex configuration backed up:"
                echo "$BACKUP_FILE"

            fi

            cat > "$CODEX_CONFIG" <<EOF
model = "$MODEL"
model_provider = "agentrouter"

[model_providers.agentrouter]
name = "AgentRouter"
base_url = "https://agentrouter.org/v1"
wire_api = "responses"
requires_openai_auth = false
experimental_bearer_token = "$AGENTROUTER_KEY"
EOF

            chmod 600 "$CODEX_CONFIG"

            echo
            echo "Codex configured."
            echo "Model: $MODEL"
            echo "Config: $CODEX_CONFIG"

        fi

    else

        echo "Codex skipped."

    fi

fi

# ============================================================
# DEEPSEEK / ZHIPU
# ============================================================

if [ "$PROVIDER" = "DeepSeek" ] || [ "$PROVIDER" = "Zhipu AI" ]; then

    echo
    echo "============================================================"
    echo "$PROVIDER"
    echo "============================================================"
    echo
    echo "Selected model:"
    echo "$MODEL"
    echo
    echo "AgentRouter environment configuration has been saved."
    echo
    echo "You can use this model through AgentRouter-compatible"
    echo "applications/tools."
    echo

fi

# ============================================================
# FINAL
# ============================================================

echo
echo "============================================================"
echo "                    SETUP COMPLETE"
echo "============================================================"
echo

echo "Provider:"
echo "$PROVIDER"

echo

echo "Model:"
echo "$MODEL"

echo

echo "AgentRouter:"
echo "https://agentrouter.org/v1"

echo

echo "Configuration saved to:"
echo "$SHELL_CONFIG"

if [ -f "$HOME/.codex/config.toml" ] && [ "$PROVIDER" = "OpenAI" ]; then
    echo
    echo "Codex configuration:"
    echo "$HOME/.codex/config.toml"
fi

echo
echo "============================================================"
echo
echo "Reload your shell with:"
echo
echo "source \"$SHELL_CONFIG\""
echo
echo "============================================================"
echo "Done."
echo "============================================================"
echo