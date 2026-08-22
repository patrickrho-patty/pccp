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

record NaverProfile(String subject, String email, String name, String nickname, String profileImage) {
    static NaverProfile parse(JsonNode root) {
        if (root == null || !"00".equals(text(root, "resultcode"))) {
            throw new IllegalArgumentException("Naver profile response was not successful");
        }
        JsonNode response = root.get("response");
        String subject = text(response, "id");
        if (subject == null || subject.isBlank()) {
            throw new IllegalArgumentException("Naver profile response has no immutable subject");
        }
        return new NaverProfile(
                subject,
                text(response, "email"),
                text(response, "name"),
                text(response, "nickname"),
                text(response, "profile_image"));
    }

    String username() {
        return email == null || email.isBlank() ? "naver:" + subject : email;
    }

    private static String text(JsonNode node, String field) {
        if (node == null || node.isNull()) {
            return null;
        }
        JsonNode value = node.get(field);
        return value == null || value.isNull() ? null : value.asText();
    }
}
