package kubernetes

import (
	"regexp"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"

	"github.com/rusenask/keel/types"
	"github.com/rusenask/keel/util/policies"
	"github.com/rusenask/keel/util/version"

	log "github.com/Sirupsen/logrus"
)

// ProviderName - provider name
const ProviderName = "kubernetes"

var versionreg = regexp.MustCompile(`:[^:]*$`)

// Provider - kubernetes provider for auto update
type Provider struct {
	implementer Implementer

	events chan *types.Event
	stop   chan struct{}
}

// NewProvider - create new kubernetes based provider
func NewProvider(implementer Implementer) (*Provider, error) {
	return &Provider{
		implementer: implementer,
		events:      make(chan *types.Event, 100),
		stop:        make(chan struct{}),
	}, nil
}

// Submit - submit event to provider
func (p *Provider) Submit(event types.Event) error {
	p.events <- &event
	return nil
}

// GetName - get provider name
func (p *Provider) GetName() string {
	return ProviderName
}

// Start - starts kubernetes provider, waits for events
func (p *Provider) Start() error {
	return p.startInternal()
}

// Stop - stops kubernetes provider
func (p *Provider) Stop() {
	close(p.stop)
}

func (p *Provider) startInternal() error {
	for {
		select {
		case event := <-p.events:
			log.WithFields(log.Fields{
				"repository": event.Repository.Name,
				"tag":        event.Repository.Tag,
				"registry":   event.Repository.Host,
			}).Info("provider.kubernetes: processing event")
			_, err := p.processEvent(event)
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
					"image": event.Repository.Name,
					"tag":   event.Repository.Tag,
				}).Error("provider.kubernetes: failed to process event")
			}
		case <-p.stop:
			log.Info("provider.kubernetes: got shutdown signal, stopping...")
			return nil
		}
	}
}

func (p *Provider) processEvent(event *types.Event) (updated []*v1beta1.Deployment, err error) {
	impacted, err := p.impactedDeployments(&event.Repository)
	if err != nil {
		return nil, err
	}

	if len(impacted) == 0 {
		log.WithFields(log.Fields{
			"image": event.Repository.Name,
			"tag":   event.Repository.Tag,
		}).Info("provider.kubernetes: no impacted deployments found for this event")
		return
	}

	return p.updateDeployments(impacted)
}

func (p *Provider) updateDeployments(deployments []v1beta1.Deployment) (updated []*v1beta1.Deployment, err error) {
	for _, deployment := range deployments {
		err := p.implementer.Update(&deployment)
		if err != nil {
			log.WithFields(log.Fields{
				"error":      err,
				"namespace":  deployment.Namespace,
				"deployment": deployment.Name,
			}).Error("provider.kubernetes: got error while update deployment")
			continue
		}
		log.WithFields(log.Fields{
			"name":      deployment.Name,
			"namespace": deployment.Namespace,
		}).Info("provider.kubernetes: deployment updated")
		updated = append(updated, &deployment)
	}

	return
}

// getDeployment - helper function to get specific deployment
func (p *Provider) getDeployment(namespace, name string) (*v1beta1.Deployment, error) {
	return p.implementer.Deployment(namespace, name)
}

// gets impacted deployments by changed repository
func (p *Provider) impactedDeployments(repo *types.Repository) ([]v1beta1.Deployment, error) {

	deploymentLists, err := p.deployments()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("provider.kubernetes: failed to get deployment lists")
		return nil, err
	}

	impacted := []v1beta1.Deployment{}

	for _, deploymentList := range deploymentLists {
		for _, deployment := range deploymentList.Items {

			labels := deployment.GetLabels()

			policy := policies.GetPolicy(labels)
			if policy == types.PolicyTypeNone {
				// skip
				continue
			}

			newVersion, err := version.GetVersion(repo.Tag)
			if err != nil {
				// failed to get new version tag
				if policy == types.PolicyTypeForce {
					updated, shouldUpdateDeployment, err := p.checkUnversionedDeployment(policy, repo, deployment)
					if err != nil {
						log.WithFields(log.Fields{
							"error":      err,
							"deployment": deployment.Name,
							"namespace":  deployment.Namespace,
						}).Error("provider.kubernetes: got error while checking unversioned deployment")
						continue
					}

					if shouldUpdateDeployment {
						impacted = append(impacted, updated)
					}

					// success, unversioned deployment marked for update
					continue
				}

				log.WithFields(log.Fields{
					"error":          err,
					"repository_tag": repo.Tag,
					"deployment":     deployment.Name,
					"namespace":      deployment.Namespace,
					"policy":         policy,
				}).Warn("provider.kubernetes: got error while parsing repository tag")
				continue
			}

			updated, shouldUpdateDeployment, err := p.checkVersionedDeployment(newVersion, policy, repo, deployment)
			if err != nil {
				log.WithFields(log.Fields{
					"error":      err,
					"deployment": deployment.Name,
					"namespace":  deployment.Namespace,
				}).Error("provider.kubernetes: got error while checking versioned deployment")
				continue
			}

			if shouldUpdateDeployment {
				impacted = append(impacted, updated)
			}
		}
	}

	return impacted, nil
}

func (p *Provider) namespaces() (*v1.NamespaceList, error) {
	return p.implementer.Namespaces()
}

// deployments - gets all deployments
func (p *Provider) deployments() ([]*v1beta1.DeploymentList, error) {
	deployments := []*v1beta1.DeploymentList{}

	n, err := p.namespaces()
	if err != nil {
		return nil, err
	}

	for _, n := range n.Items {
		l, err := p.implementer.Deployments(n.GetName())
		if err != nil {
			log.WithFields(log.Fields{
				"error":     err,
				"namespace": n.GetName(),
			}).Error("provider.kubernetes: failed to list deployments")
			continue
		}
		deployments = append(deployments, l)
	}

	return deployments, nil
}
