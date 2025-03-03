/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"fmt"

	httptransport "github.com/go-openapi/runtime/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/maltejk/provider-metakube/apis/v1alpha1"

	metakube "github.com/maltejk/metakube-go-client/pkg/client"
)

const (
	errNoProviderConfigRef            = "no providerConfigRef is given"
	errCannotGetProvider              = "cannot get referenced Provider"
	errCannotTrackProviderConfigUsage = "cannot track ProviderConfig usage"
	errOnlySecretSourceAllowed        = "only Secret supported as Source"
	errExtractSecret                  = "cannot extract credentials from secret"
	errExtractSecretKey               = "cannot extract secret key"
	errGetCredentialsSecret           = "cannot get credentials secret"
	errInvalidSecretData              = "'%s' is required in secret data"
)

// GetConfig constructs an *httptransport.Runtime that can be used to connect to Metakube
// API by the Metakube client.
func GetConfig(ctx context.Context, c client.Client, mg resource.Managed) (*httptransport.Runtime, error) {
	switch {
	case mg.GetProviderConfigReference() != nil:
		return UseProviderConfig(ctx, c, mg)
	default:
		return nil, errors.New(errNoProviderConfigRef)
	}
}

// UseProviderConfig to produce a *httptransport.Runtime that can be used to connect to Metakube.
func UseProviderConfig(ctx context.Context, c client.Client, mg resource.Managed) (*httptransport.Runtime, error) { // nolint:gocyclo
	pc := &v1alpha1.ProviderConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: mg.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errCannotGetProvider)
	}

	t := resource.NewProviderConfigUsageTracker(c, &v1alpha1.ProviderConfigUsage{})
	if err := t.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errCannotTrackProviderConfigUsage)
	}

	token, credsErr := extractCredentialsFromSecret(ctx, c, pc.Spec.Token.CommonCredentialSelectors)
	if credsErr != nil {
		return nil, errors.Wrap(credsErr, errExtractSecret)
	}

	if pc.Spec.Token.Source != xpv1.CredentialsSourceSecret {
		return nil, errors.New(errOnlySecretSourceAllowed)
	}

	transport := httptransport.New("localhost", "/", metakube.DefaultSchemes)
	transport.DefaultAuthentication = httptransport.BearerToken(token.token)

	// Enable this line to see request and response in console output
	transport.SetDebug(true)

	return transport, nil
}

type providerCredentials struct {
	token string
}

func extractCredentialsFromSecret(ctx context.Context, client client.Client, s xpv1.CommonCredentialSelectors) (*providerCredentials, error) {
	if s.SecretRef == nil {
		return nil, errors.New(errExtractSecretKey)
	}
	secret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: s.SecretRef.Namespace, Name: s.SecretRef.Name}, secret); err != nil {
		return nil, errors.Wrap(err, errGetCredentialsSecret)
	}

	token := secret.Data[s.SecretRef.Key]

	if token == nil {
		return nil, errors.New(fmt.Sprintf(errInvalidSecretData, s.SecretRef.Key))
	}

	creds := &providerCredentials{
		token: string(token),
	}

	return creds, nil
}
