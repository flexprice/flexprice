#!/bin/bash

add_local_bin_to_path() {
    # Add ~/.local/bin to PATH if it's not already there
    if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
        export PATH="$HOME/.local/bin:$PATH"

        # Check which shell is being used and update the appropriate config file
        if [ -n "$ZSH_VERSION" ]; then
            echo 'export PATH="$HOME/.local/bin:$PATH"' >>~/.zshrc
            echo "Added ~/.local/bin to your PATH in ~/.zshrc. Please run 'source ~/.zshrc' to update your current session."
        else
            echo 'export PATH="$HOME/.local/bin:$PATH"' >>~/.bashrc
            echo "Added ~/.local/bin to your PATH in ~/.bashrc. Please run 'source ~/.bashrc' to update your current session."
        fi
    fi
}

create_local_bin_folder() {
    # Create ~/.local/bin if it does not exist
    if [ ! -d "$HOME/.local/bin" ]; then
        mkdir -p "$HOME/.local/bin"
        echo "Created ~/.local/bin directory."
    fi
}

if ! which typst >/dev/null; then
    ARCH=$(uname -m)
    OS=$(uname)
    if [ "$OS" = "Darwin" ]; then
        echo "Installing typst binary for Darwin"
        if [ "$ARCH" = "arm64" ]; then
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-aarch64-apple-darwin.tar.xz -o typst.tar.xz
        else
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-x86_64-apple-darwin.tar.xz -o typst.tar.xz
        fi
        create_local_bin_folder
        mkdir -p typst-darwin && tar -xf typst.tar.xz -C typst-darwin --strip-components=1 && mv typst-darwin/typst ~/.local/bin/ && rm -rf typst.tar.xz typst-darwin
        chmod +x ~/.local/bin/typst
        add_local_bin_to_path
    elif [ "$OS" = "Linux" ]; then
        echo "Installing typst binary for Linux"
        if [ "$ARCH" = "aarch64" ]; then
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-aarch64-unknown-linux-musl.tar.xz -o typst.tar.xz
        elif [ "$ARCH" = "x86_64" ]; then
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-x86_64-unknown-linux-musl.tar.xz -o typst.tar.xz
        elif [ "$ARCH" = "armv7" ]; then
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-armv7-unknown-linux-musleabi.tar.xz -o typst.tar.xz
        elif [ "$ARCH" = "riscv64" ]; then
            curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-riscv64gc-unknown-linux-gnu.tar.xz -o typst.tar.xz
        fi
        create_local_bin_folder
        mkdir -p typst-linux && tar -xf typst.tar.xz -C typst-linux --strip-components=1 && mv typst-linux/typst ~/.local/bin/ && rm -rf typst.tar.xz typst-linux
        chmod +x ~/.local/bin/typst
        add_local_bin_to_path
    elif [ "$OS" = "CYGWIN" ] || [ "$OS" = "MINGW" ]; then
        echo "Installing typst binary for Windows"
        curl -L https://github.com/typst/typst/releases/download/v0.15.1/typst-x86_64-pc-windows-msvc.zip -o typst.zip
        create_local_bin_folder
        mkdir -p typst-windows && unzip typst.zip -d typst-windows && mv typst-windows/typst.exe ~/.local/bin/ && rm -rf typst.zip typst-windows
        chmod +x ~/.local/bin/typst.exe
        add_local_bin_to_path
    fi
fi
