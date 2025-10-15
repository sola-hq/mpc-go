#!/bin/bash
set -e

CRED_PATH="/etc/mpc/mpc-db-password.cred"
HOST_SECRET="/var/lib/systemd/credential.secret"

# Check if systemd-creds is available
if ! command -v systemd-creds >/dev/null 2>&1; then
    echo "❌ systemd-creds command not found"
    echo "This script requires systemd credentials support (systemd v250+)."
    echo "Please upgrade systemd or use a newer Linux distribution."
    exit 1
fi

# Ensure systemd host secret exists
if [ ! -f "$HOST_SECRET" ]; then
    echo "🔑 Generating systemd host secret..."
    sudo systemd-creds setup
fi

echo "🔑 Using host secret encryption"

# Ensure /etc/mpc exists
sudo mkdir -p /etc/mpc
sudo chmod 700 /etc/mpc
sudo chown root:root /etc/mpc

echo "🔒 PASSWORD REQUIREMENTS & RECOMMENDATIONS:"
echo "• Password must be EXACTLY 16, 24, or 32 characters long"
echo "• Use a password manager (Bitwarden, 1Password, LastPass, etc.) to generate a unique password"
echo "• Each mpc node should use a DIFFERENT password for security"
echo "• Use special characters: !@#$^&*()-_=+[]{}|;:,.<>?/~"
echo "• Examples:"
echo "  - 16 chars: 'P@ssw0rd12345678'"
echo "  - 24 chars: 'MySecure!Pass@24Bytes12'" 
echo "  - 32 chars: 'VeryLong&Secure#Password!32Chars1'"
echo

# Prompt for password twice
while true; do
    read -s -r -p "🔐 Enter mpc database password: " PASSWORD
    echo
    read -s -r -p "🔐 Confirm password: " CONFIRM
    echo
    if [ "$PASSWORD" = "$CONFIRM" ]; then
        # Validate password length
        PASSWORD_LEN=${#PASSWORD}
        if [ $PASSWORD_LEN -ne 16 ] && [ $PASSWORD_LEN -ne 24 ] && [ $PASSWORD_LEN -ne 32 ]; then
            echo "❌ Password must be exactly 16, 24, or 32 characters long (current: $PASSWORD_LEN). Try again."
            continue
        fi
        break
    else
        echo "❌ Passwords do not match. Try again."
    fi
done

# Display masked password and backup warning
PASSWORD_LEN=${#PASSWORD}
if [ $PASSWORD_LEN -gt 4 ]; then
    MASKED_PASSWORD="${PASSWORD:0:2}$(printf '%*s' $((PASSWORD_LEN-4)) | tr ' ' '*')${PASSWORD: -2}"
else
    MASKED_PASSWORD=$(printf '%*s' $PASSWORD_LEN | tr ' ' '*')
fi

echo
echo "⚠️  IMPORTANT BACKUP WARNING ⚠️"
echo "Your password: $MASKED_PASSWORD"
echo "Please backup this password securely - if lost, encrypted data cannot be recovered!"
echo "Double-check the password above is correct before continuing."
echo

# Check if identity was generated with encryption
echo
read -p "❓ Did you use 'mpc-cli generate-identity --encrypt' to generate your identity with encryption mode? (y/n): " USE_ENCRYPTED_IDENTITY

if [[ "$USE_ENCRYPTED_IDENTITY" =~ ^[Yy]$ ]]; then
    echo
    echo "🔐 Identity Encryption Password Required:"
    echo "• This password decrypts your node identity files"
    echo "• Must be the SAME password used with 'mpc-cli generate-identity --encrypt'"
    echo "• Without this password, the mpc node cannot start"
    echo
    
    while true; do
        read -s -r -p "🔐 Enter identity encryption password: " IDENTITY_PASSWORD
        echo
        read -s -r -p "🔐 Confirm identity password: " IDENTITY_CONFIRM
        echo
        if [ "$IDENTITY_PASSWORD" = "$IDENTITY_CONFIRM" ]; then
            break
        else
            echo "❌ Passwords do not match. Try again."
        fi
    done
    
    # Encrypt identity password with host secret - using here-doc to preserve all special characters
    IDENTITY_CRED_PATH="/etc/mpc/mpc-identity-password.cred"
    sudo systemd-creds encrypt --name=mpc-identity-password.cred - "$IDENTITY_CRED_PATH" <<< "$IDENTITY_PASSWORD"
    sudo chmod 600 "$IDENTITY_CRED_PATH"
    sudo chown root:root "$IDENTITY_CRED_PATH"
    
    echo "✅ Identity password credential generated at $IDENTITY_CRED_PATH"
    echo
fi

# Encrypt with host secret - using here-doc to preserve all special characters
sudo systemd-creds encrypt --name=mpc-db-password.cred - "$CRED_PATH" <<< "$PASSWORD"

sudo chmod 600 "$CRED_PATH"
sudo chown root:root "$CRED_PATH"

# Print blob for SetCredentialEncrypted
echo
echo "✅ Credential blob generated at $CRED_PATH"
echo
