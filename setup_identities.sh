#!/bin/bash

# Identity Setup Script
# This script sets up identities for testing with separate test database paths
set -e

# Cross-platform sed in-place function
# macOS requires backup extension, Linux doesn't
replace() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        sed -i '' "$@"
    else
        # Linux and others
        sed -i "$@"
    fi
}

# Number of test nodes
NUM_NODES=3
BASE_DIR="$(pwd)"
TEST_DB_PATH="$BASE_DIR/test_db"

echo "🚀 Setting up E2E Test Node Identities..."

# Generate random password for badger encryption
echo "🔐 Generating random password for badger encryption..."
BADGER_PASSWORD=$(openssl rand -base64 32 | tr -d '=+/' | head -c 32)
echo "✅ Generated password: $BADGER_PASSWORD"

# Generate config.yaml from template
echo "📝 Generating config.yaml from template..."
if [ ! -f "config.yaml.template" ]; then
    echo "❌ Template file config.yaml.template not found"
    exit 1
fi

# Create a temporary config with placeholder values (will be updated later with real pubkey)
TEMP_PUBKEY="0000000000000000000000000000000000000000000000000000000000000000"

# Escape special characters in password for sed
ESCAPED_PASSWORD=$(printf '%s\n' "$BADGER_PASSWORD" | sed 's/[[\.*^$()+?{|]/\\&/g')

sed -e "s/{{\.BadgerPassword}}/$ESCAPED_PASSWORD/g" \
    -e "s/{{\.EventInitiatorPubkey}}/$TEMP_PUBKEY/g" \
    config.yaml.template > config.yaml

echo "✅ Generated config.yaml from template"

# Clean up any existing test data
echo "🧹 Cleaning up existing test data..."
rm -rf "$TEST_DB_PATH"
rm -rf "$BASE_DIR"/node*

# Create test node directories
echo "📁 Creating test node directories..."
# Generate UUIDs for the nodes
NODE0_UUID=$(uuidgen)
NODE1_UUID=$(uuidgen)
NODE2_UUID=$(uuidgen)

for i in $(seq 0 $((NUM_NODES-1))); do
    mkdir -p "$BASE_DIR/node$i/identity"
    cp "$BASE_DIR/config.yaml.template" "$BASE_DIR/node$i/config.yaml"
    
    # Create peers.json with proper UUIDs
    cat > "$BASE_DIR/node$i/peers.json" << EOF
{
  "node0": "$NODE0_UUID",
  "node1": "$NODE1_UUID",
  "node2": "$NODE2_UUID"
}
EOF
done

# Generate identity for each test node
echo "🔑 Generating identities for each test node..."
for i in $(seq 0 $((NUM_NODES-1))); do
    echo "📝 Generating identity for node$i..."
    cd "$BASE_DIR/node$i"
    
    # Generate identity using mpc-cli
    mpc-cli identity generate --node "node$i"
    
    cd - > /dev/null
done

# Distribute identity files to all test nodes
echo "🔄 Distributing identity files across test nodes..."
for i in $(seq 0 $((NUM_NODES-1))); do
    for j in $(seq 0 $((NUM_NODES-1))); do
        if [ $i != $j ]; then
            echo "📋 Copying node${i}_identity.json to node$j..."
            cp "$BASE_DIR/node$i/identity/node${i}_identity.json" "$BASE_DIR/node$j/identity/"
        fi
    done
done

echo "🔄 register peers..."
mpc-cli peer register --config ./config.yaml --environment development

# Generate test event initiator
echo "🔐 Generating test event initiator..."
cd "$BASE_DIR"
mpc-cli initiator generate --node-name event_initiator --output-dir . --overwrite

# Extract the public key from the generated identity
if [ -f "event_initiator.identity.json" ]; then
    PUBKEY=$(cat event_initiator.identity.json | jq -r '.public_key')
    echo "📝 Updating config files with event initiator public key and password..."
    
    # Update all test node config files with the actual public key and password
    for i in $(seq 0 $((NUM_NODES-1))); do
        # Update public key using sed with | as delimiter (safer than /)
        replace "s|event_initiator_pubkey:.*|event_initiator_pubkey: $PUBKEY|g" "$BASE_DIR/node$i/config.yaml"
        # Update password using sed with | as delimiter and escaped password
        replace "s|badger_password:.*|badger_password: $ESCAPED_PASSWORD|g" "$BASE_DIR/node$i/config.yaml"
    done
    
    # Also update the main config.yaml.template
    replace "s|event_initiator_pubkey:.*|event_initiator_pubkey: $PUBKEY|g" "$BASE_DIR/config.yaml.template"
    replace "s|badger_password:.*|badger_password: $ESCAPED_PASSWORD|g" "$BASE_DIR/config.yaml.template"
    
    echo "✅ Event initiator public key updated: $PUBKEY"
    echo "✅ Badger password updated: $BADGER_PASSWORD"
else
    echo "❌ Failed to generate event initiator identity"
    exit 1
fi

cd - > /dev/null

echo "✨ Node identities setup complete!"
echo
echo "📂 Created folder structure:"
echo "├── node0"
echo "│   ├── config.yaml"
echo "│   ├── identity/"
echo "│   └── peers.json"
echo "├── node1"
echo "│   ├── config.yaml"
echo "│   ├── identity/"
echo "│   └── peers.json"
echo "└── node2"
echo "    ├── config.yaml"
echo "    ├── identity/"
echo "    └── peers.json"
echo
echo "✅ You can now start your nodes with:"
echo "cd node0 && mpc-cli node start -n node0"
echo "cd node1 && mpc-cli node start -n node1"
echo "cd node2 && mpc-cli node start -n node2" 