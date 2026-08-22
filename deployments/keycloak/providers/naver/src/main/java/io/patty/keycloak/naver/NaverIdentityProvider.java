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

import com.fasterxml.jackson.databind.JsonNode;
import jakarta.ws.rs.core.Response;
import org.keycloak.broker.oidc.AbstractOAuth2IdentityProvider;
import org.keycloak.broker.oidc.OAuth2IdentityProviderConfig;
import org.keycloak.broker.oidc.mappers.AbstractJsonUserAttributeMapper;
import org.keycloak.broker.provider.BrokeredIdentityContext;
import org.keycloak.broker.provider.IdentityBrokerException;
import org.keycloak.broker.social.SocialIdentityProvider;
import org.keycloak.events.EventBuilder;
import org.keycloak.http.simple.SimpleHttp;
import org.keycloak.http.simple.SimpleHttpResponse;
import org.keycloak.models.KeycloakSession;

public final class NaverIdentityProvider
        extends AbstractOAuth2IdentityProvider<OAuth2IdentityProviderConfig>
        implements SocialIdentityProvider<OAuth2IdentityProviderConfig> {
    static final String AUTHORIZATION_URL = "https://nid.naver.com/oauth2.0/authorize";
    static final String TOKEN_URL = "https://nid.naver.com/oauth2.0/token";
    static final String PROFILE_URL = "https://openapi.naver.com/v1/nid/me";

    public NaverIdentityProvider(KeycloakSession session, OAuth2IdentityProviderConfig config) {
        super(session, config);
        config.setAuthorizationUrl(AUTHORIZATION_URL);
        config.setTokenUrl(TOKEN_URL);
        config.setUserInfoUrl(PROFILE_URL);
    }

    @Override
    protected boolean supportsExternalExchange() {
        return false;
    }

    @Override
    protected String getProfileEndpointForValidation(EventBuilder event) {
        return PROFILE_URL;
    }

    @Override
    protected BrokeredIdentityContext extractIdentityFromProfile(EventBuilder event, JsonNode root) {
        NaverProfile profile = NaverProfile.parse(root);
        BrokeredIdentityContext identity = new BrokeredIdentityContext(profile.subject(), getConfig());
        identity.setUsername(profile.username());
        identity.setEmail(profile.email());
        identity.setName(profile.name());
        identity.setIdp(this);
        if (profile.nickname() != null) {
            identity.setUserAttribute("naver.nickname", profile.nickname());
        }
        if (profile.profileImage() != null) {
            identity.setUserAttribute("naver.profile_image", profile.profileImage());
        }
        AbstractJsonUserAttributeMapper.storeUserProfileForMapper(identity, root, getConfig().getAlias());
        return identity;
    }

    @Override
    protected BrokeredIdentityContext doGetFederatedIdentity(String accessToken) {
        try (SimpleHttpResponse response = SimpleHttp.create(session).doGet(PROFILE_URL)
                .header("Authorization", "Bearer " + accessToken)
                .header("Accept", "application/json")
                .asResponse()) {
            if (Response.Status.fromStatusCode(response.getStatus()).getFamily()
                    != Response.Status.Family.SUCCESSFUL) {
                throw new IdentityBrokerException("Naver profile endpoint rejected the access token");
            }
            return extractIdentityFromProfile(null, response.asJson());
        } catch (IdentityBrokerException e) {
            throw e;
        } catch (Exception e) {
            throw new IdentityBrokerException("Could not obtain the Naver profile", e);
        }
    }

    @Override
    protected String getDefaultScopes() {
        return "name email profile_image";
    }
}
