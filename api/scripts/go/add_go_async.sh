#!/bin/bash

# Script to copy the async.go file to the API SDK directory

# Get the directory of this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
SDK_DIR="$SCRIPT_DIR/../../go"
SOURCE_FILE="$SCRIPT_DIR/async.go"
TARGET_FILE="$SDK_DIR/async.go"

# Verify source file exists
if [ ! -f "$SOURCE_FILE" ]; then
    echo "Error: Source file not found: $SOURCE_FILE"
    exit 1
fi

# Verify SDK directory exists
if [ ! -d "$SDK_DIR" ]; then
    echo "Error: SDK directory not found: $SDK_DIR"
    exit 1
fi

# Copy the file
echo "Copying $SOURCE_FILE to $TARGET_FILE"
cp "$SOURCE_FILE" "$TARGET_FILE"

if [ $? -eq 0 ]; then
    echo "Successfully added async functionality to the FlexPrice Go SDK"
else
    echo "Error: Failed to copy the file"
    exit 1
fi