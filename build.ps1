# AI Proxy Build Script (PowerShell)

$OutputBinary = "ai-proxy.exe"
$SourceFile = "main.go"

Write-Host "--- Starting AI Proxy Build ---" -ForegroundColor Cyan

# Check if Go is installed
if (!(Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go is not installed or not in PATH." -ForegroundColor Red
    exit 1
}

Push-Location backend

# Run build
Write-Host "Compiling $SourceFile..." -ForegroundColor Yellow
go build -o ../$OutputBinary $SourceFile

if ($LASTEXITCODE -eq 0) {
    $fileInfo = Get-Item ../$OutputBinary
    Write-Host "`nSuccessfully built: $($fileInfo.FullName)" -ForegroundColor Green
    Write-Host "Size: $([Math]::Round($fileInfo.Length / 1MB, 2)) MB" -ForegroundColor Gray
    Write-Host "Date: $($fileInfo.LastWriteTime)" -ForegroundColor Gray
    Write-Host "`nTo run the service: .\$OutputBinary" -ForegroundColor Cyan
} else {
    Write-Host "`nBuild FAILED!" -ForegroundColor Red
    Pop-Location
    exit $LASTEXITCODE
}

Pop-Location
