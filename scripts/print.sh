# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print::info() {
   printf "${GREEN}[INFO]%s${NC}\n" "$1"
}

print::warn() {
    printf "${YELLOW}[WARN]%s${NC}\n" "$1"
}

print::error() {
    printf "${RED}[ERROR]%s${NC}\n" "$1"
}
