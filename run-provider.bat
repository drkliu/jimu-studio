@echo off
setlocal EnableExtensions
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo ERROR: Go is not available on PATH.
  pause
  exit /b 1
)

powershell.exe -NoProfile -Command "try { $health = Invoke-RestMethod -Uri 'http://127.0.0.1:8081/healthz' -TimeoutSec 2; if ($health.status -eq 'ok') { exit 0 }; exit 1 } catch { exit 1 }" >nul 2>nul
if not errorlevel 1 (
  echo Local Jimu Provider is already running at http://127.0.0.1:8081
  pause
  exit /b 0
)

set "GOTELEMETRY=off"
set "GOCACHE=%~dp0.cache\go-build"
set "GOMODCACHE=%~dp0.cache\go-mod"

echo Starting local Jimu Provider...
echo   Address: http://127.0.0.1:8081
echo.
echo Keep this window open. Press Ctrl+C to stop the provider.
echo.

go run ./cmd/local-provider
set "PROVIDER_EXIT=%ERRORLEVEL%"

if not "%PROVIDER_EXIT%"=="0" (
  echo.
  echo Local provider exited with code %PROVIDER_EXIT%.
  pause
)

exit /b %PROVIDER_EXIT%
