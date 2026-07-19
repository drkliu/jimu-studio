@echo off
setlocal EnableExtensions
cd /d "%~dp0"

set "PSQL=D:\soft\PostgreSQL\17\bin\psql.exe"
if not exist "%PSQL%" for /f "delims=" %%P in ('where psql.exe 2^>nul') do if not defined PSQL_FOUND set "PSQL_FOUND=%%P"
if defined PSQL_FOUND set "PSQL=%PSQL_FOUND%"
if not exist "%PSQL%" (
  echo ERROR: psql.exe was not found. Install PostgreSQL and add its bin directory to PATH.
  pause
  exit /b 1
)

if not defined JIMU_POSTGRES_ADMIN_USER set "JIMU_POSTGRES_ADMIN_USER=postgres"
if not defined JIMU_POSTGRES_ADMIN_PASSWORD (
  for /f "usebackq delims=" %%S in (`powershell.exe -NoProfile -Command "[Environment]::GetEnvironmentVariable('JIMU_POSTGRES_ADMIN_PASSWORD','User')"`) do set "JIMU_POSTGRES_ADMIN_PASSWORD=%%S"
)
if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
  for /f "usebackq delims=" %%S in (`powershell.exe -NoProfile -Command "[Environment]::GetEnvironmentVariable('JIMU_STUDIO_POSTGRES_PASSWORD','User')"`) do set "JIMU_STUDIO_POSTGRES_PASSWORD=%%S"
)
if not defined JIMU_POSTGRES_ADMIN_PASSWORD (
  echo ERROR: Set JIMU_POSTGRES_ADMIN_PASSWORD to the password for %JIMU_POSTGRES_ADMIN_USER%.
  pause
  exit /b 1
)
if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
  echo ERROR: Set a URL-safe local password first, for example:
  echo   setx JIMU_STUDIO_POSTGRES_PASSWORD "replace-with-a-long-random-password"
  pause
  exit /b 1
)
powershell.exe -NoProfile -Command "if ($env:JIMU_STUDIO_POSTGRES_PASSWORD -notmatch '^[A-Za-z0-9._~-]{16,128}$') { exit 1 }"
if errorlevel 1 (
  echo ERROR: JIMU_STUDIO_POSTGRES_PASSWORD must be 16-128 URL-safe characters: A-Z a-z 0-9 . _ ~ -
  pause
  exit /b 1
)

set "PGPASSWORD=%JIMU_POSTGRES_ADMIN_PASSWORD%"
echo Configuring dedicated Jimu PostgreSQL roles and databases on 127.0.0.1:5432...
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\setup-postgres.ps1" -Psql "%PSQL%" -AdminUser "%JIMU_POSTGRES_ADMIN_USER%"
if errorlevel 1 (
  echo ERROR: PostgreSQL setup failed. Check the admin username/password and server permissions.
  pause
  exit /b 1
)

echo.
echo PostgreSQL is ready:
echo   Studio Provider: jimu_studio_local, role jimu_studio
echo   Dex OIDC:        jimu_dex_local, role jimu_dex
echo Restart run-provider.bat and run-oidc.bat so they use PostgreSQL.
pause
