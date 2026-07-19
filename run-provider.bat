@echo off
setlocal EnableExtensions
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo ERROR: Go is not available on PATH.
  pause
  exit /b 1
)

if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
  for /f "usebackq delims=" %%S in (`powershell.exe -NoProfile -Command "[Environment]::GetEnvironmentVariable('JIMU_STUDIO_POSTGRES_PASSWORD','User')"`) do set "JIMU_STUDIO_POSTGRES_PASSWORD=%%S"
)
if not defined JIMU_STUDIO_POSTGRES_DSN (
  if not defined JIMU_STUDIO_POSTGRES_PASSWORD (
    echo ERROR: PostgreSQL is not configured. Run setup-postgres.bat first.
    pause
    exit /b 1
  )
  set "JIMU_STUDIO_POSTGRES_DSN=postgres://jimu_studio:%JIMU_STUDIO_POSTGRES_PASSWORD%@127.0.0.1:5432/jimu_studio_local?sslmode=disable"
)

powershell.exe -NoProfile -Command "try { $health = Invoke-RestMethod -Uri 'http://127.0.0.1:8081/healthz' -TimeoutSec 2; if ($health.status -eq 'ok' -and $health.storage -eq 'postgresql') { exit 0 }; if ($health.status -eq 'ok') { exit 2 }; exit 1 } catch { exit 1 }" >nul 2>nul
set "HEALTH_EXIT=%ERRORLEVEL%"
if "%HEALTH_EXIT%"=="0" (
  echo Local Jimu Provider is already running at http://127.0.0.1:8081
  pause
  exit /b 0
)
if "%HEALTH_EXIT%"=="2" (
  echo ERROR: An older non-PostgreSQL Provider is already using port 8081.
  echo Stop its window with Ctrl+C, then run this file again.
  pause
  exit /b 1
)

set "GOTELEMETRY=off"
set "GOCACHE=%~dp0.cache\go-build"
set "GOMODCACHE=%~dp0.cache\go-mod"

echo Starting local Jimu Provider...
echo   Address: http://127.0.0.1:8081
echo   Storage: PostgreSQL database jimu_studio_local
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
