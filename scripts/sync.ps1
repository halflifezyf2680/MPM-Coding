# Sync and Push script for MPM project

$CommitMessage = $args[0]
if (-not $CommitMessage) {
    $CommitMessage = "Update project: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
}

Write-Host "🚀 Starting Sync Process..." -ForegroundColor Cyan

# 1. Check for untracked files first
Write-Host "🔍 Checking for untracked files..." -ForegroundColor Yellow
$untrackedFiles = git status --porcelain | Where-Object { $_ -match '^\?\?' }
if ($untrackedFiles) {
    Write-Host "❌ ERROR: Untracked files detected. Please handle them manually or add to .gitignore:" -ForegroundColor Red
    $untrackedFiles | ForEach-Object { Write-Host "   $_" -ForegroundColor Red }
    Write-Host "🛑 Aborting sync to prevent accidental commits of untracked files." -ForegroundColor Yellow
    exit 1
}

# 2. Stage tracked modifications/deletions only
Write-Host "📦 Staging tracked modifications..." -ForegroundColor Yellow
git add -u

# 3. Commit
Write-Host "📝 Committing changes with message: '$CommitMessage'..." -ForegroundColor Yellow
git commit -m $CommitMessage

# 4. Push
Write-Host "📤 Pushing to remote repository (main)..." -ForegroundColor Yellow
git push origin main

if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ Sync completed successfully!" -ForegroundColor Green
} else {
    Write-Host "❌ Sync failed. Please check the errors above." -ForegroundColor Red
}
