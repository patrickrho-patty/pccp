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

import org.keycloak.broker.oidc.mappers.AbstractJsonUserAttributeMapper;

public final class NaverUserAttributeMapper extends AbstractJsonUserAttributeMapper {
    private static final String[] COMPATIBLE_PROVIDERS = {NaverIdentityProviderFactory.PROVIDER_ID};

    @Override
    public String[] getCompatibleProviders() {
        return COMPATIBLE_PROVIDERS.clone();
    }

    @Override
    public String getId() {
        return "naver-user-attribute-mapper";
    }
}
