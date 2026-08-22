package io.patty.keycloak.naver;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

class NaverProfileTest {
    private final ObjectMapper mapper = new ObjectMapper();

    @Test
    void parsesStableSubjectAndOptionalAttributes() throws Exception {
        var profile = NaverProfile.parse(mapper.readTree("""
            {"resultcode":"00","message":"success","response":{
              "id":"naver-subject-1","email":"user@example.com","name":"Patty User",
              "nickname":"Patty","profile_image":"https://example.com/avatar.png"
            }}
            """));

        assertEquals("naver-subject-1", profile.subject());
        assertEquals("user@example.com", profile.email());
        assertEquals("Patty User", profile.name());
        assertEquals("Patty", profile.nickname());
        assertEquals("https://example.com/avatar.png", profile.profileImage());
        assertEquals("user@example.com", profile.username());
    }

    @Test
    void permitsMissingEmailWithoutTurningEmailIntoIdentity() throws Exception {
        var profile = NaverProfile.parse(mapper.readTree("""
            {"resultcode":"00","message":"success","response":{"id":"naver-subject-2"}}
            """));

        assertEquals("naver:naver-subject-2", profile.username());
        assertNull(profile.email());
    }

    @Test
    void rejectsFailedOrSubjectlessResponses() throws Exception {
        assertThrows(IllegalArgumentException.class, () -> NaverProfile.parse(mapper.readTree("""
            {"resultcode":"024","message":"authentication failed"}
            """)));
        assertThrows(IllegalArgumentException.class, () -> NaverProfile.parse(mapper.readTree("""
            {"resultcode":"00","message":"success","response":{"email":"user@example.com"}}
            """)));
    }
}
