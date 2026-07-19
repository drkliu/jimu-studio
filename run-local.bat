@echo off
setlocal EnableExtensions

cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo ERROR: Go is not available on PATH.
  echo Install Go 1.26.5, open a new terminal, and try again.
  pause
  exit /b 1
)

if /I "%~1"=="test" goto :test
if /I "%~1"=="--test" goto :test

set "STUDIO_RUN_CONFIG=%STUDIO_CONFIG%"
if not "%~1"=="" set "STUDIO_RUN_CONFIG=%~1"
if not defined STUDIO_RUN_CONFIG set "STUDIO_RUN_CONFIG=%~dp0studio.local.json"

for %%I in ("%STUDIO_RUN_CONFIG%") do set "STUDIO_CONFIG=%%~fI"

if not exist "%STUDIO_CONFIG%" (
  echo ERROR: Studio configuration was not found:
  echo   %STUDIO_CONFIG%
  echo.
  echo Usage:
  echo   run-local.bat "D:\path\to\studio-config.json"
  echo.
  echo Or set STUDIO_CONFIG before running this file.
  echo See docs\configuration.md for the required JSON fields.
  pause
  exit /b 1
)

if not defined STUDIO_ADDRESS set "STUDIO_ADDRESS=127.0.0.1:8080"

echo Starting Jimu Studio...
echo   Address: http://%STUDIO_ADDRESS%
echo   Config:  %STUDIO_CONFIG%
echo.
echo Press Ctrl+C to stop Studio.
echo.

go run ./cmd/studio
set "STUDIO_EXIT=%ERRORLEVEL%"

if not "%STUDIO_EXIT%"=="0" (
  echo.
  echo Studio exited with code %STUDIO_EXIT%.
  echo Check that the config uses loopback endpoints with development=true
  echo and that every referenced OIDC client-secret environment variable is set.
  pause
)

exit /b %STUDIO_EXIT%

:test
echo Running Jimu Studio local verification...
echo.

if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
  for /f "usebackq delims=" %%S in (`powershell.exe -NoProfile -Command "[Environment]::GetEnvironmentVariable('JIMU_STUDIO_POSTGRES_PASSWORD','User')"`) do set "JIMU_STUDIO_POSTGRES_PASSWORD=%%S"
)
if not defined JIMU_TEST_PG_DSN (
  if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
    echo ERROR: PostgreSQL is not configured. Run setup-postgres.bat first.
    pause
    exit /b 1
  )
  set "JIMU_TEST_PG_DSN=postgres://jimu_studio:%JIMU_STUDIO_POSTGRES_PASSWORD%@127.0.0.1:5432/jimu_studio_local?sslmode=disable"
)

go test ./... -count=1
if errorlevel 1 goto :test_failed

go test -race ./... -count=1
if errorlevel 1 goto :test_failed

go vet ./...
if errorlevel 1 goto :test_failed

if not exist ".cache" mkdir ".cache"
go build -o ".cache\studio-local.exe" ./cmd/studio
if errorlevel 1 goto :test_failed

go test -tags=e2e -timeout=3m ./e2e
if errorlevel 1 goto :test_failed

echo.
echo All local verification passed.
pause
exit /b 0

:test_failed
echo.
echo Local verification failed with code %ERRORLEVEL%.
echo If only Chrome startup failed, run this file from a normal PowerShell or
echo Command Prompt outside a restricted sandbox.
pause
exit /b 1
