# ArgoCD Project Operator

A Kubebuilder-based operator for managing ArgoCD Projects with template-based configuration, designed for multi-tenant platforms.

## Overview

This operator simplifies ArgoCD Project management by:
- **Template-based configuration**: Choose from `standard`, `privileged`, or `restricted` templates
- **Simplified CRD**: Expose only essential fields (name, repositories, destinations)
- **GitOps-friendly**: Export rendered AppProjects for promotion across environments
- **Multi-tenant ready**: Designed for platforms managing 100+ tenants

## Architecture

```
ManagedArgoCDProject (CRD)
    ↓
Operator Controller
    ↓ (renders using templates)
ArgoCD AppProject
    ↓ (export to Git)
GitOps Repository → Stage/Prod Clusters
```

## Prerequisites

- Go 1.21+
- Kubebuilder 3.x
- kubectl
- Access to a Kubernetes cluster with ArgoCD installed

## Quick Start

### 1. Replace Files in Your Repo

Replace the following files in your `argocd-project-operator` repo:

**API Types** (`api/v1alpha1/managedargoCDproject_types.go`):
```bash
# Copy the enhanced types file
cp managedargoCDproject_types.go <your-repo>/api/v1alpha1/managedargoCDproject_types.go
```

**Controller** (`internal/controller/managedargoCDproject_controller.go`):
```bash
# Copy the controller with template logic
cp managedargoCDproject_controller.go <your-repo>/internal/controller/managedargoCDproject_controller.go
```

### 2. Generate CRDs and Manifests

```bash
cd <your-repo>

# Generate the CRD manifests
make manifests

# Generate Go code (DeepCopy methods)
make generate
```

### 3. Install CRDs

```bash
# Install CRDs into your cluster
make install

# Verify CRD is installed
kubectl get crd managedargoCDprojects.argocd.platform.io
```

### 4. Run the Operator

**Option A: Run locally (for development)**
```bash
make run
```

**Option B: Deploy to cluster**
```bash
# Build and push the image
make docker-build docker-push IMG=<your-registry>/argocd-project-operator:v0.1.0

# Deploy to cluster
make deploy IMG=<your-registry>/argocd-project-operator:v0.1.0
```

### 5. Create a ManagedArgoCDProject

```bash
# Apply one of the samples
kubectl apply -f config/samples/sample-standard.yaml

# Check the status
kubectl get macp -n argocd
kubectl describe macp tenant-app-standard -n argocd

# Verify the AppProject was created
kubectl get appproject tenant-app -n argocd
```

## Templates

### Standard Template (Default)
- **Use case**: Regular application teams
- **Permissions**: Deploy apps, manage configs/secrets, limited cluster resources
- **Roles**: read-only, developer, admin

```yaml
spec:
  template: standard
```

### Privileged Template
- **Use case**: Platform teams, infrastructure management
- **Permissions**: Full cluster access, all resource types
- **Roles**: platform-admin with full permissions

```yaml
spec:
  template: privileged
```

### Restricted Template
- **Use case**: External contractors, untrusted teams
- **Permissions**: Limited to Deployments, Services, ConfigMaps
- **Roles**: read-only only

```yaml
spec:
  template: restricted
```

## Multi-Environment Workflow

### Development Environment
1. Create `ManagedArgoCDProject` in dev cluster:
   ```bash
   kubectl apply -f tenant-app-project.yaml
   ```

2. Operator creates ArgoCD AppProject immediately

3. Test and validate the configuration

### Export to Git for Stage/Prod
4. Export the rendered AppProject:
   ```bash
   ./export-projects.sh -o ~/gitops-repo/projects/
   ```

5. Review and commit to Git:
   ```bash
   cd ~/gitops-repo/projects
   git add tenant-app-project.yaml
   git commit -m "feat: add tenant-app ArgoCD project"
   git push
   ```

6. ArgoCD syncs to stage/prod from Git (standard GitOps)

### Alternative: Direct Export from Status

```bash
# Get rendered YAML from status
kubectl get macp tenant-app-standard -n argocd -o jsonpath='{.status.renderedYAML}' > tenant-app-project.yaml

# Clean up and commit
yq eval 'del(.metadata.ownerReferences)' -i tenant-app-project.yaml
git add tenant-app-project.yaml
git commit -m "feat: add tenant-app project"
```

## Example Manifests

### Standard Tenant Project
```yaml
apiVersion: argocd.platform.io/v1alpha1
kind: ManagedArgoCDProject
metadata:
  name: tenant-app-standard
  namespace: argocd
spec:
  projectName: tenant-app
  repositories:
    - https://github.com/your-org/app-configs
    - https://github.com/your-org/helm-charts
  destinations:
    - server: https://kubernetes.default.svc
      namespace: tenant-app-dev
    - server: https://kubernetes.default.svc
      namespace: tenant-app-prod
  template: standard
  description: "Tenant application project"
```

### Platform Team Project
```yaml
apiVersion: argocd.platform.io/v1alpha1
kind: ManagedArgoCDProject
metadata:
  name: platform-infra
  namespace: argocd
spec:
  projectName: platform-infrastructure
  repositories:
    - https://github.com/platform/infrastructure
    - https://github.com/platform/crossplane-configs
  destinations:
    - server: https://kubernetes.default.svc
      namespace: "*"
  template: privileged
  description: "Platform team infrastructure management"
```

## Export Tool Usage

The `export-projects.sh` script helps extract rendered AppProjects for GitOps:

```bash
# Export all projects
./export-projects.sh

# Export to specific directory
./export-projects.sh -o /path/to/gitops-repo/argocd-projects

# Clean previous exports
./export-projects.sh -c -o ./exports

# Export from different namespace
./export-projects.sh -n argocd-staging -o ./staging-exports
```

## Customizing Templates

Templates are defined in the controller code (`internal/controller/managedargoCDproject_controller.go`). To add or modify templates:

1. Edit the `loadTemplate()` function
2. Add your template configuration
3. Rebuild and redeploy the operator

Example template structure:
```go
"custom-template": {
    "clusterResourceWhitelist": []map[string]string{
        {"group": "apps", "kind": "Deployment"},
    },
    "namespaceResourceWhitelist": []map[string]string{
        {"group": "*", "kind": "*"},
    },
    "roles": []map[string]interface{}{
        {
            "name": "custom-role",
            "policies": []string{
                "p, proj:{{PROJECT}}:custom-role, applications, *, {{PROJECT}}/*, allow",
            },
        },
    },
},
```

## Monitoring and Troubleshooting

### Check Operator Logs
```bash
# If running locally
# Logs are in terminal

# If deployed to cluster
kubectl logs -n argocd-project-operator-system deployment/argocd-project-operator-controller-manager -f
```

### Check Resource Status
```bash
# List all managed projects
kubectl get macp -A

# Describe a specific project
kubectl describe macp <name> -n argocd

# Check conditions
kubectl get macp <name> -n argocd -o jsonpath='{.status.conditions}'
```

### Common Issues

**AppProject not created**:
- Check operator logs
- Verify RBAC permissions: `kubectl auth can-i create appprojects --as=system:serviceaccount:argocd-project-operator-system:argocd-project-operator-controller-manager -n argocd`
- Ensure ArgoCD CRDs are installed

**Template not found**:
- Verify template name in spec matches available templates (standard, privileged, restricted)
- Check controller logs for template loading errors

## Development

### Running Tests
```bash
make test
```

### Local Development Loop
```bash
# 1. Make changes to code
# 2. Regenerate manifests
make manifests generate

# 3. Run locally
make run

# 4. Test with sample
kubectl apply -f config/samples/sample-standard.yaml
```

### Building and Deploying
```bash
# Build image
make docker-build IMG=myregistry/argocd-project-operator:dev

# Push image
make docker-push IMG=myregistry/argocd-project-operator:dev

# Deploy
make deploy IMG=myregistry/argocd-project-operator:dev
```

## Integration with Your Platform

### With Crossplane
If you're using Crossplane for tenant onboarding, you can reference ManagedArgoCDProject in your Compositions:

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
spec:
  resources:
    - name: argocd-project
      base:
        apiVersion: argocd.platform.io/v1alpha1
        kind: ManagedArgoCDProject
        spec:
          template: standard
```

### With Azure DevOps Pipelines
Generate ManagedArgoCDProjects from your tenant onboarding pipeline:

```yaml
- task: Kubernetes@1
  inputs:
    command: 'apply'
    arguments: '-f $(Build.ArtifactStagingDirectory)/tenant-project.yaml'
```

### With GitOps (ArgoCD ApplicationSet)
Use the operator in dev, export to Git, then manage via ApplicationSet in stage/prod.

## Roadmap

- [ ] ConfigMap-based template storage (external templates)
- [ ] Webhook validation for ManagedArgoCDProject
- [ ] Automated Git commit on project creation (GitOps sync controller)
- [ ] Multi-cluster AppProject synchronization
- [ ] Template versioning and migration support
- [ ] Prometheus metrics for operator health

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

Apache 2.0 - See LICENSE file for details

## Support

For issues, questions, or contributions, please open an issue on GitHub.
