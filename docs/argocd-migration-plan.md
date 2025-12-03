# ArgoCD OSS to Akuity Migration Plan
## Zero-Downtime Migration Strategy

---

## Table of Contents
1. [Pre-Migration Preparation](#pre-migration-preparation)
2. [Phase 1: Environment Setup](#phase-1-environment-setup)
3. [Phase 2: Pilot Wave](#phase-2-pilot-wave)
4. [Phase 3: Production Waves](#phase-3-production-waves)
5. [Validation & Monitoring](#validation--monitoring)
6. [Rollback Procedures](#rollback-procedures)
7. [Post-Migration Cleanup](#post-migration-cleanup)

---

## Pre-Migration Preparation

### Inventory Assessment

**Objective**: Document current state of all ArgoCD resources

```bash
# Export all current applications
kubectl get applications -n argocd -o yaml > oss-applications-backup.yaml

# Export all projects
kubectl get appprojects -n argocd -o yaml > oss-projects-backup.yaml

# List all ApplicationSets
kubectl get applicationsets -n argocd -o yaml > oss-applicationsets-backup.yaml

# Get application count per project
kubectl get applications -n argocd -o json | \
  jq -r '.items[] | .spec.project' | sort | uniq -c
```

### Risk Classification

Create an inventory with risk levels:

```bash
# Generate migration inventory
cat > migration-inventory.yaml <<'EOF'
waves:
  pilot:
    risk: low
    apps:
      - name: dev-tenant-alpha
        project: tenant-alpha
        destination: dev
        criticality: low
      - name: staging-tenant-beta
        project: tenant-beta
        destination: staging
        criticality: low
    
  wave2:
    risk: medium
    apps:
      - name: prod-tenant-gamma
        project: tenant-gamma
        destination: prod
        criticality: medium
  
  # Add more waves...
EOF
```

### Prerequisites Checklist

- [ ] Akuity account provisioned
- [ ] Akuity connected to all target AKS clusters
- [ ] Service account tokens configured in Akuity
- [ ] RBAC policies reviewed and matched to OSS
- [ ] Datadog monitoring configured for Akuity instance
- [ ] Azure DevOps webhook endpoints documented
- [ ] Communication plan with stakeholders
- [ ] Rollback procedures tested in non-prod

---

## Phase 1: Environment Setup

### 1.1 Akuity Cluster Registration

```bash
#!/bin/bash
# register-clusters.sh
# Register your AKS clusters with Akuity

AKUITY_ORG="your-org"
AKUITY_TOKEN="your-token"

CLUSTERS=(
  "aks-prod-eastus"
  "aks-prod-westus"
  "aks-staging-eastus"
  "aks-dev-eastus"
)

for cluster in "${CLUSTERS[@]}"; do
  echo "Registering cluster: $cluster"
  
  # Get cluster credentials
  az aks get-credentials --name $cluster --resource-group your-rg
  
  # Register with Akuity (using Akuity CLI)
  akuity argocd cluster add $cluster \
    --organization $AKUITY_ORG \
    --insecure-skip-tls-verify=false
done
```

### 1.2 Project Replication

Create ArgoCD Projects in Akuity matching your OSS configuration:

```bash
#!/bin/bash
# replicate-projects.sh
# Export projects from OSS and import to Akuity

NAMESPACE="argocd"
AKUITY_ORG="your-org"

# Get all projects except default
projects=$(kubectl get appprojects -n $NAMESPACE -o json | \
  jq -r '.items[] | select(.metadata.name != "default") | .metadata.name')

for project in $projects; do
  echo "Replicating project: $project"
  
  # Export project spec (remove managed fields)
  kubectl get appproject $project -n $NAMESPACE -o json | \
    jq 'del(.metadata.managedFields, .metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.generation)' \
    > "/tmp/${project}-project.json"
  
  # Import to Akuity (via API or CLI)
  akuity argocd appproject create \
    --organization $AKUITY_ORG \
    --file "/tmp/${project}-project.json"
done
```

### 1.3 Repository Credentials

```bash
#!/bin/bash
# setup-repo-credentials.sh
# Configure repository access in Akuity

# Export repo secrets from OSS
kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repository -o yaml > repo-secrets-backup.yaml

# For Azure DevOps repos, you'll need to configure via Akuity UI or API
# This typically involves PAT tokens or SSH keys

echo "Repository credentials need to be configured in Akuity UI"
echo "Required repositories:"
kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repository -o jsonpath='{.items[*].data.url}' | base64 -d
```

---

## Phase 2: Pilot Wave

### 2.1 Application Migration Script

```bash
#!/bin/bash
# migrate-application.sh
# Migrates a single application from OSS to Akuity with zero downtime

set -e

APP_NAME=$1
NAMESPACE="argocd"
AKUITY_ORG="your-org"
DRY_RUN=${2:-false}

if [ -z "$APP_NAME" ]; then
  echo "Usage: $0 <application-name> [dry-run]"
  exit 1
fi

echo "================================================"
echo "Migrating Application: $APP_NAME"
echo "Dry Run: $DRY_RUN"
echo "================================================"

# Step 1: Export application from OSS
echo -e "\n[1/7] Exporting application from OSS..."
kubectl get application $APP_NAME -n $NAMESPACE -o json > "/tmp/${APP_NAME}-oss.json"

if [ $? -ne 0 ]; then
  echo "ERROR: Application $APP_NAME not found in OSS ArgoCD"
  exit 1
fi

# Extract key information
PROJECT=$(jq -r '.spec.project' "/tmp/${APP_NAME}-oss.json")
SOURCE_REPO=$(jq -r '.spec.source.repoURL' "/tmp/${APP_NAME}-oss.json")
DEST_NAMESPACE=$(jq -r '.spec.destination.namespace' "/tmp/${APP_NAME}-oss.json")

echo "  Project: $PROJECT"
echo "  Source: $SOURCE_REPO"
echo "  Destination: $DEST_NAMESPACE"

# Step 2: Verify application is healthy
echo -e "\n[2/7] Verifying application health..."
HEALTH_STATUS=$(kubectl get application $APP_NAME -n $NAMESPACE -o jsonpath='{.status.health.status}')
SYNC_STATUS=$(kubectl get application $APP_NAME -n $NAMESPACE -o jsonpath='{.status.sync.status}')

echo "  Health: $HEALTH_STATUS"
echo "  Sync: $SYNC_STATUS"

if [ "$HEALTH_STATUS" != "Healthy" ]; then
  echo "WARNING: Application is not healthy. Continue? (y/n)"
  read -r response
  if [ "$response" != "y" ]; then
    echo "Aborting migration"
    exit 1
  fi
fi

# Step 3: Create application in Akuity with sync disabled
echo -e "\n[3/7] Creating shadow application in Akuity (sync disabled)..."

# Prepare Akuity application manifest
jq 'del(.metadata.managedFields, .metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.generation, .status) | 
    .spec.syncPolicy.automated = null | 
    .metadata.name = .metadata.name' \
    "/tmp/${APP_NAME}-oss.json" > "/tmp/${APP_NAME}-akuity.json"

if [ "$DRY_RUN" = "true" ]; then
  echo "  [DRY RUN] Would create application in Akuity"
  cat "/tmp/${APP_NAME}-akuity.json"
else
  # Create via Akuity CLI or API
  akuity argocd app create \
    --organization $AKUITY_ORG \
    --file "/tmp/${APP_NAME}-akuity.json"
  
  echo "  ✓ Application created in Akuity (sync disabled)"
fi

# Step 4: Verify Akuity can see the application
echo -e "\n[4/7] Verifying Akuity application status..."
sleep 5

if [ "$DRY_RUN" = "false" ]; then
  akuity argocd app get $APP_NAME --organization $AKUITY_ORG
fi

# Step 5: Disable auto-sync on OSS application
echo -e "\n[5/7] Disabling auto-sync on OSS application..."

if [ "$DRY_RUN" = "true" ]; then
  echo "  [DRY RUN] Would disable auto-sync on OSS"
else
  kubectl patch application $APP_NAME -n $NAMESPACE \
    --type merge \
    -p '{"spec":{"syncPolicy":{"automated":null}}}'
  
  echo "  ✓ Auto-sync disabled on OSS"
fi

# Step 6: Wait for stability
echo -e "\n[6/7] Waiting for stability (60 seconds)..."
if [ "$DRY_RUN" = "false" ]; then
  for i in {60..1}; do
    echo -ne "  Waiting... $i seconds remaining\r"
    sleep 1
  done
  echo -e "\n  ✓ Stability period complete"
fi

# Step 7: Enable auto-sync on Akuity application
echo -e "\n[7/7] Enabling auto-sync on Akuity application..."

if [ "$DRY_RUN" = "true" ]; then
  echo "  [DRY RUN] Would enable auto-sync on Akuity"
else
  # Enable via Akuity CLI or API
  akuity argocd app set $APP_NAME \
    --organization $AKUITY_ORG \
    --sync-policy automated \
    --auto-prune \
    --self-heal
  
  echo "  ✓ Auto-sync enabled on Akuity"
  
  # Trigger initial sync
  echo "  Triggering initial sync..."
  akuity argocd app sync $APP_NAME --organization $AKUITY_ORG
fi

echo -e "\n================================================"
echo "Migration Steps Completed!"
echo "================================================"
echo ""
echo "NEXT STEPS:"
echo "1. Monitor Akuity application for 24-48 hours"
echo "2. Verify application health: akuity argocd app get $APP_NAME"
echo "3. Check application logs and metrics in Datadog"
echo "4. If successful, run cleanup: ./cleanup-oss-app.sh $APP_NAME"
echo ""
echo "ROLLBACK:"
echo "Run: ./rollback-application.sh $APP_NAME"
echo ""
```

### 2.2 Validation Script

```bash
#!/bin/bash
# validate-migration.sh
# Validates application migration was successful

APP_NAME=$1
NAMESPACE="argocd"
AKUITY_ORG="your-org"

if [ -z "$APP_NAME" ]; then
  echo "Usage: $0 <application-name>"
  exit 1
fi

echo "Validating Migration: $APP_NAME"
echo "=================================="

# Check OSS status
echo -e "\n[OSS ArgoCD]"
OSS_SYNC=$(kubectl get application $APP_NAME -n $NAMESPACE -o jsonpath='{.spec.syncPolicy.automated}' 2>/dev/null)
if [ -z "$OSS_SYNC" ] || [ "$OSS_SYNC" = "null" ]; then
  echo "  ✓ Auto-sync disabled"
else
  echo "  ✗ Auto-sync still enabled!"
fi

# Check Akuity status
echo -e "\n[Akuity ArgoCD]"
AKUITY_STATUS=$(akuity argocd app get $APP_NAME --organization $AKUITY_ORG -o json)

HEALTH=$(echo $AKUITY_STATUS | jq -r '.status.health.status')
SYNC=$(echo $AKUITY_STATUS | jq -r '.status.sync.status')
AUTO_SYNC=$(echo $AKUITY_STATUS | jq -r '.spec.syncPolicy.automated')

echo "  Health: $HEALTH"
echo "  Sync: $SYNC"
echo "  Auto-sync: $AUTO_SYNC"

if [ "$HEALTH" = "Healthy" ] && [ "$AUTO_SYNC" != "null" ]; then
  echo -e "\n✓ Migration validation PASSED"
  exit 0
else
  echo -e "\n✗ Migration validation FAILED"
  exit 1
fi
```

### 2.3 Pilot Wave Execution

```bash
#!/bin/bash
# execute-pilot-wave.sh
# Runs the pilot wave migration

PILOT_APPS=(
  "dev-tenant-alpha"
  "staging-tenant-beta"
)

echo "Starting Pilot Wave Migration"
echo "=============================="
echo "Applications: ${PILOT_APPS[@]}"
echo ""

for app in "${PILOT_APPS[@]}"; do
  echo "Processing: $app"
  
  # Run migration
  ./migrate-application.sh $app false
  
  if [ $? -eq 0 ]; then
    echo "✓ $app migration initiated"
    
    # Wait before next app
    echo "Waiting 5 minutes before next migration..."
    sleep 300
  else
    echo "✗ $app migration failed"
    echo "Aborting pilot wave"
    exit 1
  fi
done

echo ""
echo "Pilot Wave Complete!"
echo "===================="
echo "Monitor applications for 24-48 hours before proceeding to Wave 2"
echo ""
echo "Validation commands:"
for app in "${PILOT_APPS[@]}"; do
  echo "  ./validate-migration.sh $app"
done
```

---

## Phase 3: Production Waves

### 3.1 Wave Orchestration Script

```bash
#!/bin/bash
# execute-wave.sh
# Executes a migration wave with parallel processing

WAVE_FILE=$1
MAX_PARALLEL=${2:-5}
WAIT_BETWEEN=${3:-300}  # seconds

if [ -z "$WAVE_FILE" ]; then
  echo "Usage: $0 <wave-file.txt> [max-parallel] [wait-between-seconds]"
  exit 1
fi

if [ ! -f "$WAVE_FILE" ]; then
  echo "ERROR: Wave file not found: $WAVE_FILE"
  exit 1
fi

echo "Executing Migration Wave"
echo "========================"
echo "Wave file: $WAVE_FILE"
echo "Max parallel: $MAX_PARALLEL"
echo ""

# Read applications from file
mapfile -t APPS < "$WAVE_FILE"

total=${#APPS[@]}
current=0
failed=0

for app in "${APPS[@]}"; do
  # Skip empty lines and comments
  [[ -z "$app" || "$app" =~ ^# ]] && continue
  
  current=$((current + 1))
  echo "[$current/$total] Migrating: $app"
  
  # Run migration
  ./migrate-application.sh "$app" false > "logs/migration-${app}.log" 2>&1 &
  
  # Wait if we hit parallel limit
  if [ $(jobs -r | wc -l) -ge $MAX_PARALLEL ]; then
    echo "  Waiting for parallel jobs to complete..."
    wait -n
  fi
  
  # Check if migration succeeded
  if [ $? -ne 0 ]; then
    echo "  ✗ Migration failed for $app"
    failed=$((failed + 1))
  else
    echo "  ✓ Migration initiated for $app"
  fi
  
  # Wait between migrations
  if [ $current -lt $total ]; then
    echo "  Waiting ${WAIT_BETWEEN}s before next migration..."
    sleep $WAIT_BETWEEN
  fi
done

# Wait for all background jobs
echo "Waiting for all migrations to complete..."
wait

echo ""
echo "Wave Execution Complete"
echo "======================="
echo "Total: $total"
echo "Failed: $failed"
echo ""

if [ $failed -gt 0 ]; then
  echo "⚠ Some migrations failed. Review logs in logs/ directory"
  exit 1
else
  echo "✓ All migrations successful"
  exit 0
fi
```

### 3.2 Wave Definition Files

Create wave files listing applications to migrate:

```bash
# wave2.txt
prod-tenant-delta
prod-tenant-epsilon
prod-tenant-zeta
staging-tenant-eta
staging-tenant-theta
# ... 20 total apps

# wave3.txt
# Next batch...
```

---

## Validation & Monitoring

### 4.1 Continuous Validation Script

```bash
#!/bin/bash
# monitor-migration.sh
# Continuous monitoring of migration status

AKUITY_ORG="your-org"
INTERVAL=${1:-60}  # seconds

echo "Starting Migration Monitor (Ctrl+C to stop)"
echo "==========================================="

while true; do
  clear
  echo "Migration Status - $(date)"
  echo "==========================================="
  
  # Count OSS apps with auto-sync disabled
  oss_disabled=$(kubectl get applications -n argocd -o json | \
    jq '[.items[] | select(.spec.syncPolicy.automated == null)] | length')
  
  # Get Akuity app status
  akuity_healthy=$(akuity argocd app list --organization $AKUITY_ORG -o json | \
    jq '[.[] | select(.status.health.status == "Healthy")] | length')
  
  akuity_synced=$(akuity argocd app list --organization $AKUITY_ORG -o json | \
    jq '[.[] | select(.status.sync.status == "Synced")] | length')
  
  akuity_total=$(akuity argocd app list --organization $AKUITY_ORG -o json | jq 'length')
  
  echo "OSS ArgoCD:"
  echo "  Apps with auto-sync disabled: $oss_disabled"
  echo ""
  echo "Akuity ArgoCD:"
  echo "  Total applications: $akuity_total"
  echo "  Healthy: $akuity_healthy"
  echo "  Synced: $akuity_synced"
  echo ""
  
  if [ "$akuity_healthy" -eq "$akuity_total" ] && [ "$akuity_synced" -eq "$akuity_total" ]; then
    echo "✓ All applications healthy and synced"
  else
    echo "⚠ Some applications need attention"
    
    # Show unhealthy apps
    echo ""
    echo "Unhealthy Applications:"
    akuity argocd app list --organization $AKUITY_ORG -o json | \
      jq -r '.[] | select(.status.health.status != "Healthy") | .metadata.name'
  fi
  
  echo ""
  echo "Next refresh in ${INTERVAL}s..."
  sleep $INTERVAL
done
```

### 4.2 Datadog Monitoring Dashboard

```yaml
# datadog-dashboard.yaml
# Import this into Datadog for migration monitoring

dashboard:
  title: "ArgoCD Migration Dashboard"
  widgets:
    - definition:
        title: "Applications by Instance"
        type: "query_value"
        requests:
          - q: "sum:argocd.app.count{instance:oss}"
            aggregator: "last"
        custom_unit: "apps"
    
    - definition:
        title: "Akuity Sync Status"
        type: "timeseries"
        requests:
          - q: "sum:argocd.app.health.status{instance:akuity} by {status}"
    
    - definition:
        title: "Migration Progress"
        type: "query_value"
        requests:
          - q: "(sum:argocd.app.count{instance:akuity} / (sum:argocd.app.count{instance:oss} + sum:argocd.app.count{instance:akuity})) * 100"
        custom_unit: "%"
    
    - definition:
        title: "Failed Syncs"
        type: "timeseries"
        requests:
          - q: "sum:argocd.app.sync.failed{instance:akuity}"
```

---

## Rollback Procedures

### 5.1 Application Rollback Script

```bash
#!/bin/bash
# rollback-application.sh
# Rolls back a single application from Akuity to OSS

set -e

APP_NAME=$1
NAMESPACE="argocd"
AKUITY_ORG="your-org"

if [ -z "$APP_NAME" ]; then
  echo "Usage: $0 <application-name>"
  exit 1
fi

echo "================================================"
echo "Rolling Back Application: $APP_NAME"
echo "================================================"

# Step 1: Disable Akuity sync immediately
echo -e "\n[1/4] Disabling Akuity auto-sync..."
akuity argocd app set $APP_NAME \
  --organization $AKUITY_ORG \
  --sync-policy none

echo "  ✓ Akuity sync disabled"

# Step 2: Re-enable OSS sync
echo -e "\n[2/4] Re-enabling OSS auto-sync..."
kubectl patch application $APP_NAME -n $NAMESPACE \
  --type merge \
  -p '{"spec":{"syncPolicy":{"automated":{"prune":true,"selfHeal":true}}}}'

echo "  ✓ OSS sync re-enabled"

# Step 3: Trigger OSS sync
echo -e "\n[3/4] Triggering OSS sync..."
kubectl -n $NAMESPACE patch application $APP_NAME \
  --type merge \
  -p '{"operation":{"sync":{"revision":"HEAD"}}}'

echo "  ✓ OSS sync triggered"

# Step 4: Verify
echo -e "\n[4/4] Verifying rollback..."
sleep 10

HEALTH=$(kubectl get application $APP_NAME -n $NAMESPACE -o jsonpath='{.status.health.status}')
SYNC=$(kubectl get application $APP_NAME -n $NAMESPACE -o jsonpath='{.status.sync.status}')

echo "  OSS Health: $HEALTH"
echo "  OSS Sync: $SYNC"

echo -e "\n================================================"
echo "Rollback Complete!"
echo "================================================"
echo ""
echo "Monitor the application in OSS ArgoCD"
echo "The Akuity application can be deleted once stability is confirmed"
```

### 5.2 Wave Rollback Script

```bash
#!/bin/bash
# rollback-wave.sh
# Rolls back an entire wave of applications

WAVE_FILE=$1

if [ -z "$WAVE_FILE" ]; then
  echo "Usage: $0 <wave-file.txt>"
  exit 1
fi

echo "Rolling Back Wave: $WAVE_FILE"
echo "=============================="

mapfile -t APPS < "$WAVE_FILE"

for app in "${APPS[@]}"; do
  [[ -z "$app" || "$app" =~ ^# ]] && continue
  
  echo "Rolling back: $app"
  ./rollback-application.sh "$app"
  
  if [ $? -eq 0 ]; then
    echo "✓ $app rolled back"
  else
    echo "✗ $app rollback failed"
  fi
  
  echo "---"
done

echo "Wave rollback complete"
```

---

## Post-Migration Cleanup

### 6.1 Cleanup OSS Applications

```bash
#!/bin/bash
# cleanup-oss-app.sh
# Safely removes application from OSS after successful migration

APP_NAME=$1
NAMESPACE="argocd"
RETENTION_DAYS=${2:-7}

if [ -z "$APP_NAME" ]; then
  echo "Usage: $0 <application-name> [retention-days]"
  exit 1
fi

echo "Cleanup: $APP_NAME"
echo "=================="

# Verify migration date
MIGRATION_DATE=$(kubectl get application $APP_NAME -n $NAMESPACE \
  -o jsonpath='{.metadata.annotations.migration-date}' 2>/dev/null)

if [ -z "$MIGRATION_DATE" ]; then
  echo "WARNING: No migration date found"
  echo "Annotate first: kubectl annotate application $APP_NAME migration-date=$(date -I) -n $NAMESPACE"
  exit 1
fi

# Calculate days since migration
CURRENT_DATE=$(date +%s)
MIG_DATE=$(date -d "$MIGRATION_DATE" +%s)
DAYS_DIFF=$(( (CURRENT_DATE - MIG_DATE) / 86400 ))

echo "Migration date: $MIGRATION_DATE"
echo "Days since migration: $DAYS_DIFF"
echo "Retention period: $RETENTION_DAYS days"

if [ $DAYS_DIFF -lt $RETENTION_DAYS ]; then
  echo "⚠ Retention period not met. Wait $(( RETENTION_DAYS - DAYS_DIFF )) more days"
  exit 1
fi

# Verify Akuity app is healthy
echo "Verifying Akuity application health..."
AKUITY_HEALTH=$(akuity argocd app get $APP_NAME --organization your-org -o json | \
  jq -r '.status.health.status')

if [ "$AKUITY_HEALTH" != "Healthy" ]; then
  echo "✗ Akuity application not healthy. Aborting cleanup."
  exit 1
fi

echo "✓ Akuity application healthy"

# Delete OSS application (orphan resources)
echo "Deleting OSS application (orphaning resources)..."
kubectl delete application $APP_NAME -n $NAMESPACE --cascade=orphan

echo "✓ Cleanup complete for $APP_NAME"
```

### 6.2 Final Cleanup Script

```bash
#!/bin/bash
# final-cleanup.sh
# Complete cleanup after all migrations

NAMESPACE="argocd"
BACKUP_DIR="./oss-backup-$(date +%Y%m%d)"

echo "Final Migration Cleanup"
echo "======================="

# Create backup directory
mkdir -p $BACKUP_DIR

# Backup remaining OSS resources
echo "Creating final backup..."
kubectl get applications -n $NAMESPACE -o yaml > "$BACKUP_DIR/applications.yaml"
kubectl get appprojects -n $NAMESPACE -o yaml > "$BACKUP_DIR/projects.yaml"
kubectl get applicationsets -n $NAMESPACE -o yaml > "$BACKUP_DIR/applicationsets.yaml"

echo "✓ Backup created in $BACKUP_DIR"

# List remaining OSS applications
REMAINING=$(kubectl get applications -n $NAMESPACE --no-headers | wc -l)
echo ""
echo "Remaining OSS applications: $REMAINING"

if [ $REMAINING -gt 0 ]; then
  echo "Applications still in OSS:"
  kubectl get applications -n $NAMESPACE -o custom-columns=NAME:.metadata.name,PROJECT:.spec.project,SYNC:.spec.syncPolicy.automated
  echo ""
  echo "⚠ Not all applications migrated. Review before decommissioning OSS."
else
  echo "✓ All applications migrated"
  echo ""
  echo "You can now safely decommission the OSS ArgoCD instance"
fi
```

---

## Migration Timeline

### Recommended Schedule for 100 Applications

| Week | Phase | Activities | Apps Migrated |
|------|-------|------------|---------------|
| 1 | Preparation | Environment setup, testing | 0 |
| 2 | Pilot | 2-3 low-risk apps | 3 |
| 3 | Validation | Monitor pilot, refine process | 0 |
| 4 | Wave 2 | Non-critical production | 20 |
| 5 | Wave 3 | Standard production | 25 |
| 6 | Wave 4 | Standard production | 25 |
| 7 | Wave 5 | High-priority production | 20 |
| 8 | Final | Critical apps + cleanup | 7 |

**Total Duration**: 8 weeks for complete migration

---

## Troubleshooting Guide

### Common Issues

**Issue**: Akuity application shows "OutOfSync"
```bash
# Solution: Check if source changed during migration
akuity argocd app diff <app-name> --organization your-org

# Force sync if needed
akuity argocd app sync <app-name> --organization your-org --force
```

**Issue**: OSS application won't disable auto-sync
```bash
# Solution: Direct edit
kubectl edit application <app-name> -n argocd
# Remove spec.syncPolicy.automated section
```

**Issue**: Resources orphaned during migration
```bash
# Solution: Both ArgoCD instances should use --cascade=orphan
# Resources remain managed until Akuity takes over
```

---

## Success Criteria

✓ Zero application downtime during migration  
✓ All applications healthy in Akuity  
✓ No manual intervention required by tenants  
✓ Datadog metrics show no degradation  
✓ Azure DevOps webhooks functioning  
✓ Kong API gateways routing correctly  
✓ Cert-manager certificates renewing  
✓ RBAC policies enforced correctly

---

## Emergency Contacts

- **Akuity Support**: support@akuity.io
- **Internal Escalation**: [Your escalation path]
- **Datadog On-Call**: [Your on-call]

---

## Notes

- All scripts assume `akuity` CLI is installed and configured
- Adjust `AKUITY_ORG` variable in all scripts
- Test all scripts in dev/staging before production
- Keep OSS instance running 2 weeks after final migration
- Maintain backups of OSS configuration
