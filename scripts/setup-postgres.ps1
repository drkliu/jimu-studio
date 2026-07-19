param(
    [Parameter(Mandatory = $true)]
    [string]$Psql,
    [Parameter(Mandatory = $true)]
    [string]$AdminUser,
    [int]$Port = 5432
)

$ErrorActionPreference = 'Stop'
$password = $env:JIMU_STUDIO_POSTGRES_PASSWORD
if ($password -notmatch '^[A-Za-z0-9._~-]{16,128}$') {
    throw 'JIMU_STUDIO_POSTGRES_PASSWORD is not URL-safe.'
}

$sql = @"
DO `$`$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'jimu_studio') THEN
        CREATE ROLE jimu_studio LOGIN;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'jimu_dex') THEN
        CREATE ROLE jimu_dex LOGIN;
    END IF;
END
`$`$;
ALTER ROLE jimu_studio PASSWORD '$password';
ALTER ROLE jimu_dex PASSWORD '$password';
SELECT 'CREATE DATABASE jimu_studio_local OWNER jimu_studio'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'jimu_studio_local') \gexec
SELECT 'CREATE DATABASE jimu_dex_local OWNER jimu_dex'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'jimu_dex_local') \gexec
"@

$temporarySql = Join-Path ([IO.Path]::GetTempPath()) ("jimu-postgres-setup-{0}.sql" -f [guid]::NewGuid().ToString('N'))
try {
    [IO.File]::WriteAllText($temporarySql, $sql, [Text.UTF8Encoding]::new($false))
    & $Psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -p $Port -U $AdminUser -d postgres -f $temporarySql
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}
finally {
    Remove-Item -LiteralPath $temporarySql -Force -ErrorAction SilentlyContinue
}
