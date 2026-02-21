# send-sms.ps1
# Sends an SMS message via AT commands to the Huawei modem.

param(
    [string]$PortName = "COM3",
    [int]$BaudRate = 115200,
    [string]$PhoneNumber = "",
    [string]$Message = "Hello, this is a test SMS from the SMS Gateway."
)

$sp = $null

try {
    Write-Host "========================================" -ForegroundColor White
    Write-Host "  SMS Gateway - Send Test Message" -ForegroundColor White
    Write-Host "========================================" -ForegroundColor White
    Write-Host "  Port   : $PortName" -ForegroundColor White
    Write-Host "  To     : $PhoneNumber" -ForegroundColor White
    Write-Host "  Message: $Message" -ForegroundColor White
    Write-Host "========================================" -ForegroundColor White
    Write-Host ""

    # Create serial port connection (matching working example-sms.ps1 settings)
    $sp = New-Object System.IO.Ports.SerialPort $PortName,$BaudRate,'None',8,1
    $sp.NewLine = "`r"
    $sp.ReadTimeout = 3000
    $sp.WriteTimeout = 3000

    function Send-AT ($cmd) {
        $sp.WriteLine($cmd)
        Start-Sleep -Milliseconds 300
        $response = $sp.ReadExisting()
        Write-Host ">> $cmd" -ForegroundColor Yellow
        Write-Host $response -ForegroundColor Green
        return $response
    }

    Write-Host "[*] Opening $PortName at $BaudRate baud..." -ForegroundColor Cyan
    $sp.Open()
    Write-Host "[+] Port opened successfully." -ForegroundColor Green
    Write-Host ""

    # Step 1: Test modem communication
    Write-Host "--- Step 1: Testing modem communication ---" -ForegroundColor Yellow
    $response = Send-AT "AT"
    if ($response -notmatch "OK") {
        Write-Host "[-] Modem not responding. Aborting." -ForegroundColor Red
        exit 1
    }
    Write-Host ""

    # Step 2: Set SMS text mode
    Write-Host "--- Step 2: Setting SMS text mode ---" -ForegroundColor Yellow
    $response = Send-AT "AT+CMGF=1"
    if ($response -notmatch "OK") {
        Write-Host "[-] Failed to set text mode. Aborting." -ForegroundColor Red
        exit 1
    }
    Write-Host ""

    # Step 3: Set character set to GSM
    Write-Host "--- Step 3: Setting character set ---" -ForegroundColor Yellow
    Send-AT 'AT+CSCS="GSM"'
    Write-Host ""

    # Step 4: Set SMS parameters
    Write-Host "--- Step 4: Setting SMS parameters ---" -ForegroundColor Yellow
    Send-AT "AT+CSMP=17,167,0,0"
    Write-Host ""

    # Step 5: Check network registration
    Write-Host "--- Step 5: Checking network registration ---" -ForegroundColor Yellow
    $response = Send-AT "AT+CREG?"
    if ($response -match "\+CREG: \d,1" -or $response -match "\+CREG: \d,5") {
        Write-Host "[+] Registered on network." -ForegroundColor Green
    } else {
        Write-Host "[!] Network registration unclear, attempting to send anyway..." -ForegroundColor Yellow
    }
    Write-Host ""

    # Step 6: Check signal quality
    Write-Host "--- Step 6: Checking signal quality ---" -ForegroundColor Yellow
    $response = Send-AT "AT+CSQ"
    if ($response -match "\+CSQ: (\d+),") {
        $signalStrength = [int]$Matches[1]
        if ($signalStrength -eq 99) {
            Write-Host "[!] Signal unknown or not detectable." -ForegroundColor Yellow
        } elseif ($signalStrength -lt 10) {
            Write-Host "[!] Weak signal strength: $signalStrength/31" -ForegroundColor Yellow
        } else {
            Write-Host "[+] Signal strength: $signalStrength/31" -ForegroundColor Green
        }
    }
    Write-Host ""

    # Step 7: Send the SMS
    Write-Host "--- Step 7: Sending SMS ---" -ForegroundColor Yellow
    Send-AT "AT+CMGS=""$PhoneNumber"""

    # Send message text followed by Ctrl+Z (ASCII 26)
    Write-Host "[*] Sending message body + Ctrl+Z..." -ForegroundColor Cyan
    $sp.Write($Message + [char]26)
    Start-Sleep -Seconds 5

    $response = $sp.ReadExisting()
    Write-Host $response -ForegroundColor Cyan

    if ($response -match "\+CMGS: (\d+)") {
        $messageRef = $Matches[1]
        Write-Host ""
        Write-Host "[+] SMS sent successfully! Message reference: $messageRef" -ForegroundColor Green
    } elseif ($response -match "OK") {
        Write-Host ""
        Write-Host "[+] SMS sent successfully!" -ForegroundColor Green
    } elseif ($response -match "ERROR") {
        Write-Host ""
        Write-Host "[-] Failed to send SMS. Modem returned an error." -ForegroundColor Red
    } else {
        Write-Host ""
        Write-Host "[?] Uncertain result. Check phone for delivery." -ForegroundColor Yellow
    }
}
catch [System.UnauthorizedAccessException] {
    Write-Host "[-] Access denied to $PortName. The port may be in use by another application." -ForegroundColor Red
}
catch [System.IO.IOException] {
    Write-Host "[-] IO error on $PortName. The port may not exist or the device is disconnected." -ForegroundColor Red
}
catch {
    Write-Host "[-] Error: $($_.Exception.Message)" -ForegroundColor Red
}
finally {
    if ($sp -and $sp.IsOpen) {
        $sp.Close()
        Write-Host ""
        Write-Host "[*] Port closed." -ForegroundColor Cyan
    }
}
