#!/bin/sh
set -eu
# 不开启set -x；配置值不进入命令行或日志。
MYSQL_PASSWORD="$(cat /run/secrets/mysql_password)"
SESSION_SECRET="$(cat /run/secrets/session_secret)"
export MYSQL_PASSWORD SESSION_SECRET
if [ "${1:-check}" = init-accounts ] || [ "${1:-check}" = test-accounts ] || [ "${1:-check}" = test-login ] || [ "${1:-check}" = test-search ]; then
    if [ "$1" = test-search ]; then
        export CAMPUSCLAW_MYSQL_INTEGRATION=1
        export CAMPUSCLAW_SEARCH_INTEGRATION=1
        export CAMPUSCLAW_VECTOR_SEARCH_INTEGRATION=1
        export UPLOAD_ROOT=/data/uploads
        exec /check/bin/database-tests -test.v -test.run '^(TestSearchIntegration|TestVectorSearchIntegration)$'
    fi
    # Missing files are allowed on repeat initialization; Go requires passwords only for absent users.
    for name in SEED_TEACHER_A_PASSWORD SEED_STUDENT_A_PASSWORD SEED_TEACHER_B_PASSWORD SEED_STUDENT_B_PASSWORD; do
        if [ -f "/run/seed/$name" ]; then
            export "$name=$(cat "/run/seed/$name")"
        fi
    done
    if [ "$1" = init-accounts ]; then
        exec /check/bin/campusclaw-linux --init-accounts
    fi
    export CAMPUSCLAW_MYSQL_INTEGRATION=1
    if [ "$1" = test-login ]; then
        export CAMPUSCLAW_LOGIN_INTEGRATION=1
        exec /check/bin/database-tests -test.v -test.run '^TestLoginIntegration'
    fi
    export CAMPUSCLAW_SEED_INTEGRATION=1
    exec /check/bin/database-tests -test.v -test.run '^TestSeedIntegration'
fi
if [ "${1:-check}" = init-schema ]; then
    exec /check/bin/campusclaw-linux --init-schema
fi
if [ "${1:-check}" = test-schema ]; then
    export CAMPUSCLAW_MYSQL_INTEGRATION=1
    export CAMPUSCLAW_SCHEMA_INTEGRATION=1
    exec /check/bin/database-tests -test.v -test.run '^(TestSchemaIntegration|TestAccountRegistrationIntegration|TestNgramParserIntegration|TestBuiltinAgentSeedIntegration|TestLesson5ConversationOwnershipAndContextIntegration|TestLesson5AgentAndSkillManagementIntegration|TestMCPRegistryClassIsolationCiphertextAndDisconnect)$'
fi
if [ "${1:-check}" = test-ngram ]; then
    export CAMPUSCLAW_MYSQL_INTEGRATION=1
    exec /check/bin/database-tests -test.v -test.run '^TestNgramParserIntegration$'
fi
if [ "${1:-check}" = test ]; then
    export CAMPUSCLAW_MYSQL_INTEGRATION=1
    exec /check/bin/database-tests -test.v
fi
if [ "${1:-check}" = test-materials ]; then
    export CAMPUSCLAW_MYSQL_INTEGRATION=1
    export CAMPUSCLAW_MATERIAL_INTEGRATION=1
    export CAMPUSCLAW_UPLOAD_ROOT=/data/uploads
    exec /check/bin/services-tests -test.v -test.run '^TestMaterialRecoveryIntegration'
fi
exec /check/bin/campusclaw-linux --check-db
