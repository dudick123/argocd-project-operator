#!/bin/bash
# export-projects.sh - Export rendered ArgoCD AppProjects to Git for promotion
#
# Usage:
#   ./export-projects.sh [options]
#
# Options:
#   -n, --namespace NAMESPACE    Namespace where ManagedArgoCDProjects are located (default: argocd)
#   -o, --output DIR             Output directory for exported manifests (default: ./exported-projects)
#   -c, --clean                  Clean output directory before export
#   -h, --help                   Show this help message
#
# Examples:
#   # Export all projects from argocd namespace
#   ./export-projects.sh
#
#   # Export to specific directory
#   ./export-projects.sh -o /path/to/gitops-repo/projects
#
#   # Clean and export
#   ./export-projects.sh -c -o ./projects

set -euo pipefail

# Default values
NAMESPACE="argocd"
OUTPUT_DIR="./exported-projects"
CLEAN=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -n|--namespace)
      NAMESPACE="$2"
      shift 2
      ;;
    -o|--output)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    -c|--clean)
      CLEAN=true
      shift
      ;;
    -h|--help)
      grep '^#' "$0" | tail -n +2 | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

echo -e "${GREEN}=== ArgoCD Project Exporter ===${NC}"
echo "Namespace: $NAMESPACE"
echo "Output Directory: $OUTPUT_DIR"
echo ""

# Clean output directory if requested
if [ "$CLEAN" = true ]; then
  echo -e "${YELLOW}Cleaning output directory...${NC}"
  rm -rf "$OUTPUT_DIR"
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Get all ManagedArgoCDProjects
echo -e "${GREEN}Fetching ManagedArgoCDProjects...${NC}"
PROJECTS=$(kubectl get managedargoCDprojects -n "$NAMESPACE" -o jsonpath='{.items[*].metadata.name}')

if [ -z "$PROJECTS" ]; then
  echo -e "${YELLOW}No ManagedArgoCDProjects found in namespace $NAMESPACE${NC}"
  exit 0
fi

# Export each project
EXPORTED_COUNT=0
for PROJECT in $PROJECTS; do
  echo -e "${GREEN}Processing $PROJECT...${NC}"
  
  # Get the project name from spec
  ARGOCD_PROJECT_NAME=$(kubectl get managedargoCDprojects "$PROJECT" -n "$NAMESPACE" -o jsonpath='{.spec.projectName}')
  
  # Get the actual AppProject
  if kubectl get appproject "$ARGOCD_PROJECT_NAME" -n "$NAMESPACE" &>/dev/null; then
    OUTPUT_FILE="$OUTPUT_DIR/${ARGOCD_PROJECT_NAME}-project.yaml"
    
    # Export the AppProject, cleaning up metadata
    kubectl get appproject "$ARGOCD_PROJECT_NAME" -n "$NAMESPACE" -o yaml | \
      yq eval 'del(.metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.generation, .metadata.managedFields, .metadata.ownerReferences, .status)' - | \
      yq eval '.metadata.labels."argocd.argoproj.io/secret-type" = null' - \
      > "$OUTPUT_FILE"
    
    echo -e "  ${GREEN}✓${NC} Exported to: $OUTPUT_FILE"
    ((EXPORTED_COUNT++))
  else
    echo -e "  ${RED}✗${NC} AppProject $ARGOCD_PROJECT_NAME not found"
  fi
done

echo ""
echo -e "${GREEN}=== Export Complete ===${NC}"
echo "Exported $EXPORTED_COUNT project(s) to $OUTPUT_DIR"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo "1. Review the exported manifests in $OUTPUT_DIR"
echo "2. Commit to your GitOps repository:"
echo "   cd $OUTPUT_DIR && git add . && git commit -m 'Add exported ArgoCD projects'"
echo "3. Push to Git and let ArgoCD sync to stage/prod environments"
