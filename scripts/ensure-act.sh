#!/bin/bash
# Script to ensure the 'act' CLI is installed

# Check if act is installed
if ! command -v act &> /dev/null; then
    echo "act CLI not found, installing..."
    
    # Check OS type
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        if ! command -v brew &> /dev/null; then
            echo "Homebrew not found. Please install Homebrew first: https://brew.sh/"
            exit 1
        fi
        
        brew install act
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux. Download the pinned installer to a temp file and run it as a
        # separate step (no curl|bash pipe), off a tagged release not master.
        install_sh="$(mktemp)"
        trap 'rm -f "$install_sh"' EXIT
        curl -fsSL https://raw.githubusercontent.com/nektos/act/v0.2.78/install.sh -o "$install_sh"
        sudo bash "$install_sh"
    else
        echo "Unsupported OS. Please install act manually: https://github.com/nektos/act#installation"
        exit 1
    fi
    
    echo "act CLI installed successfully"
else
    echo "act CLI is already installed"
fi

# Create examples directory if it doesn't exist for workflow
mkdir -p api/go/examples

# Check version
act --version
echo "act is ready to use"
echo "NOTE: When running act, you should use the following command:"
echo "act workflow_dispatch -e .github/workflows/test-event.json -s GITHUB_TOKEN=\"\$(cat .secrets/github-token)\" -P ubuntu-latest=catthehacker/ubuntu:act-latest --container-architecture linux/amd64" 