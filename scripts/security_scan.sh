#!/bin/bash
set -e

echo "Running security scan..."

echo "Running gosec..."
if command -v gosec &> /dev/null; then
    gosec ./...
else
    echo "gosec not found, skipping."
fi

echo "Running trivy..."
if command -v trivy &> /dev/null; then
    trivy fs .
else
    echo "trivy not found, skipping."
fi

echo "Security scan complete."
