/*
 * Copyright 2026 Patty
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */
package io.patty.keycloak.naver;

import org.keycloak.broker.oidc.OAuth2IdentityProviderConfig;
import org.keycloak.broker.provider.AbstractIdentityProviderFactory;
import org.keycloak.broker.social.SocialIdentityProviderFactory;
import org.keycloak.models.IdentityProviderModel;
import org.keycloak.models.KeycloakSession;

public final class NaverIdentityProviderFactory
        extends AbstractIdentityProviderFactory<NaverIdentityProvider>
        implements SocialIdentityProviderFactory<NaverIdentityProvider> {
    public static final String PROVIDER_ID = "naver";

    @Override
    public String getName() {
        return "Naver";
    }

    @Override
    public NaverIdentityProvider create(KeycloakSession session, IdentityProviderModel model) {
        return new NaverIdentityProvider(session, new OAuth2IdentityProviderConfig(model));
    }

    @Override
    public OAuth2IdentityProviderConfig createConfig() {
        return new OAuth2IdentityProviderConfig();
    }

    @Override
    public String getId() {
        return PROVIDER_ID;
    }
}
