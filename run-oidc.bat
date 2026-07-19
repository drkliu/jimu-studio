@echo off
setlocal EnableExtensions
cd /d "%~dp0"

set "DEX_VERSION=v2.45.1"
set "DEX_SOURCE=%~dp0.cache\dex-%DEX_VERSION%"
set "DEX_CONFIG=%~dp0dex.local.yaml"

where go >nul 2>nul
if errorlevel 1 (
  echo ERROR: Go is not available on PATH.
  echo Install Go, reopen this window, and run this file again.
  pause
  exit /b 1
)

where git >nul 2>nul
if errorlevel 1 (
  echo ERROR: Git is not available on PATH.
  echo Install Git, reopen this window, and run this file again.
  pause
  exit /b 1
)

if not exist "%DEX_CONFIG%" (
  echo ERROR: Missing Dex configuration:
  echo   %DEX_CONFIG%
  pause
  exit /b 1
)

if not defined JIMU_STUDIO_OIDC_CLIENT_SECRET (
  for /f "usebackq delims=" %%S in (`powershell.exe -NoProfile -Command "[Environment]::GetEnvironmentVariable('JIMU_STUDIO_OIDC_CLIENT_SECRET','User')"`) do set "JIMU_STUDIO_OIDC_CLIENT_SECRET=%%S"
)

if not defined JIMU_STUDIO_OIDC_CLIENT_SECRET (
  echo ERROR: JIMU_STUDIO_OIDC_CLIENT_SECRET is not configured.
  echo Set it once with:
  echo   setx JIMU_STUDIO_OIDC_CLIENT_SECRET "your-local-secret"
  echo Then run this file again.
  pause
  exit /b 1
)

powershell.exe -NoProfile -Command "try { $discovery = Invoke-RestMethod -Uri 'http://127.0.0.1:5556/.well-known/openid-configuration' -TimeoutSec 2; if ($discovery.issuer -eq 'http://127.0.0.1:5556') { exit 0 }; exit 1 } catch { exit 1 }" >nul 2>nul
if not errorlevel 1 (
  echo Local OIDC is already running at http://127.0.0.1:5556
  echo You can start run-local.bat now.
  pause
  exit /b 0
)

if not exist "%DEX_SOURCE%\.git" (
  echo Downloading Dex %DEX_VERSION% for the first run...
  git clone --depth 1 --branch %DEX_VERSION% https://github.com/dexidp/dex.git "%DEX_SOURCE%"
  if errorlevel 1 (
    echo ERROR: Could not download Dex.
    pause
    exit /b 1
  )
)

set "CGO_ENABLED=0"
set "GOTELEMETRY=off"
set "GOCACHE=%~dp0.cache\dex-go-build"
set "GOMODCACHE=%~dp0.cache\dex-go-mod"

echo.
echo Starting local OIDC...
echo   Issuer:   http://127.0.0.1:5556
echo   Config:   %DEX_CONFIG%
echo   Login:    admin@example.com / password
echo.
echo Keep this window open. Press Ctrl+C to stop OIDC.
echo Start run-local.bat in a second window.
echo.

pushd "%DEX_SOURCE%"
go run ./cmd/dex serve "%DEX_CONFIG%"
set "DEX_EXIT=%ERRORLEVEL%"
popd

if not "%DEX_EXIT%"=="0" (
  echo.
  echo OIDC exited with code %DEX_EXIT%.
  pause
)

exit /b %DEX_EXIT%
