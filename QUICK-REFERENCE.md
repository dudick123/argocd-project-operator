# ArgoCD Project Operator - Quick Reference

## Installation

```bash
# Clone and navigate to repo
git clone https://github.com/dudick123/argocd-project-operator
cd argocd-project-operator

# Install CRDs
make install

# Run operator (dev mode)
make run
```

## Common Commands

### Create Projects
```bash
# Standard project for app teams
kubectl apply -f - <<EOF
apiVersion: argocd.platform.io/v1alpha1
kind: ManagedArgoCDProject
metadata:
  name: my-app
  namespace: argocd
spec:
  projectName: my-app
  repositories:
    - https://github.com/org/repo
  destinations:
    - server: https://kubernetes.default.svc
      namespace: my-app-dev
  template: standard
EOF

# Platform team project
kubectl apply -f config/samples/sample-privileged.yaml

# Restricted contractor project
kubectl apply -f config/samples/sample-restricted.yaml
```

### View Projects
```bash
# List all managed projects
kubectl get macp -n argocd

# Get details
kubectl describe macp my-app -n argocd

# Get rendered YAML
kubectl get macp my-app -n argocd -o jsonpath='{.status.renderedYAML}'

# Watch status
kubectl get macp -n argocd -w
```

### Export for GitOps
```bash
# Export all projects to directory
./export-projects.sh -o ~/gitops-repo/projects/

# Export and clean
./export-projects.sh -c -o ./exported

# Export from custom namespace
./export-projects.sh -n my-argocd -o ./projects
```

### Direct Export (Alternative)
```bash
# Get AppProject directly
kubectl get appproject my-app -n argocd -o yaml | \
  yq eval 'del(.metadata.uid, .metadata.resourceVersion, .metadata.managedFields, .status)' - \
  > my-app-project.yaml

# Commit to Git
git add my-app-project.yaml
git commit -m "Add my-app ArgoCD project"
git push
```

### Update Projects
```bash
# Edit the ManagedArgoCDProject
kubectl edit macp my-app -n argocd

# Or apply updated YAML
kubectl apply -f my-app-updated.yaml

# Operator automatically updates the AppProject
```

### Delete Projects
```bash
# Delete ManagedArgoCDProject (cascades to AppProject)
kubectl delete macp my-app -n argocd

# Verify deletion
kubectl get appproject my-app -n argocd  # should be NotFound
```

## Templates

| Template | Use Case | Cluster Resources | RBAC Roles |
|----------|----------|-------------------|------------|
| `standard` | App teams | Namespace only | read-only, developer, admin |
| `privileged` | Platform teams | Full access | platform-admin |
| `restricted` | Contractors | None | read-only |

## Multi-Environment Workflow

**Dev Environment:**
```bash
# 1. Create in dev cluster
kubectl apply -f my-app-project.yaml

# 2. Operator creates AppProject
# 3. Test and validate
```

**Promote to Stage/Prod:**
```bash
# 4. Export rendered project
./export-projects.sh -o ~/gitops/projects/

# 5. Commit to Git
cd ~/gitops/projects
git add my-app-project.yaml
git commit -m "feat: add my-app project"
git push

# 6. ArgoCD syncs to stage/prod (GitOps)
```

## Troubleshooting

**Check operator status:**
```bash
kubectl logs -n argocd-project-operator-system \
  deployment/argocd-project-operator-controller-manager -f
```

**Verify RBAC:**
```bash
kubectl auth can-i create appprojects \
  --as=system:serviceaccount:argocd-project-operator-system:argocd-project-operator-controller-manager \
  -n argocd
```

**Check CRD:**
```bash
kubectl get crd managedargoCDprojects.argocd.platform.io
kubectl explain managedargoCDproject.spec
```

**Debug status:**
```bash
# Check conditions
kubectl get macp my-app -n argocd -o json | jq '.status.conditions'

# Check phase
kubectl get macp my-app -n argocd -o jsonpath='{.status.phase}'
```

## Template Customization

Edit `internal/controller/managedargoCDproject_controller.go`:

```go
func (r *ManagedArgoCDProjectReconciler) loadTemplate(templateName string) {
    templates := map[string]map[string]interface{}{
        "my-custom-template": {
            "clusterResourceWhitelist": []map[string]string{
                {"group": "*", "kind": "Namespace"},
            },
            // ... rest of template
        },
    }
}
```

Rebuild and redeploy:
```bash
make docker-build docker-push IMG=registry/operator:v0.2.0
make deploy IMG=registry/operator:v0.2.0
```

## Key Files

| File | Purpose |
|------|---------|
| `api/v1alpha1/managedargoCDproject_types.go` | CRD definition |
| `internal/controller/managedargoCDproject_controller.go` | Controller logic + templates |
| `config/samples/*.yaml` | Example manifests |
| `export-projects.sh` | Export tool for GitOps |

## Integration Patterns

**With Crossplane:**
```yaml
# Reference in Composition
- name: argocd-project
  base:
    apiVersion: argocd.platform.io/v1alpha1
    kind: ManagedArgoCDProject
```

**With Helm:**
```yaml
# values.yaml
projects:
  - name: app1
    repos: [...]
    dests: [...]
```

**With Kustomize:**
```yaml
# kustomization.yaml
resources:
  - managed-project.yaml
patches:
  - target:
      kind: ManagedArgoCDProject
    patch: |-
      - op: replace
        path: /spec/template
        value: privileged
```

## Best Practices

1. **Use standard template by default** - covers 80% of use cases
2. **Export to Git early** - validate renders before promoting
3. **Test in dev first** - don't create directly in prod
4. **Version your templates** - track changes in Git
5. **Document template choices** - use descriptions field
6. **Monitor operator logs** - catch issues early
7. **Backup ManagedArgoCDProjects** - they're your source of truth in dev

## Links

- [ArgoCD Project Documentation](https://argo-cd.readthedocs.io/en/stable/user-guide/projects/)
- [Kubebuilder Documentation](https://book.kubebuilder.io/)
- [GitHub Repository](https://github.com/dudick123/argocd-project-operator)
