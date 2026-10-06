# End-to-end check of profiles, proposals and access rules. Server must be running.
# Run first in this window if scripts are blocked:  Set-ExecutionPolicy -Scope Process Bypass
$api = "http://localhost:8080"
$n = Get-Random

function Call($method, $path, $body, $token) {
  $h = @{}; if ($token) { $h.Authorization = "Bearer $token" }
  $p = @{ Uri = "$api$path"; Method = $method; Headers = $h; ContentType = "application/json" }
  if ($body) { $p.Body = ($body | ConvertTo-Json) }
  try { Invoke-RestMethod @p } catch { @{ error = $true; status = [int]$_.Exception.Response.StatusCode } }
}
function Expect($label, $r, $want) {
  $got = if ($r -is [hashtable] -and $r.error) { $r.status } else { 200 }   # 200 = any 2xx
  "{0,-58} {1}" -f $label, $(if ($got -eq $want) { "OK" } else { "FAIL (got $got, want $want)" })
}
function Reg($name, $role) { Call POST /api/auth/register @{ email = "$name$n@test.com"; password = "password123"; role = $role } }

$client = Reg "client" "client"; $other = Reg "other" "client"
$d1 = Reg "dev1" "developer"; $d2 = Reg "dev2" "developer"; $d3 = Reg "dev3" "developer"

$proj = Call POST /api/projects @{ title = "Clinic booking app"; description = "We need a booking app for our clinic with push reminders and an admin panel."; budget_min = 500; budget_max = 1500 } $client.token
$projId = $proj.id    # note: $pid is a reserved PowerShell variable, do not use it
if (-not $projId) { "Could not create the project: is the server running and are the tables created?"; return }

$msg = "I have built several booking apps and can deliver this in three weeks."
$prof = @{ headline = "Android developer"; bio = "10 years"; skills = @("kotlin", "flutter"); hourly_rate = 30 }

Expect "developer saves profile" (Call PUT /api/me/profile $prof $d1.token) 200
Expect "second developer saves profile" (Call PUT /api/me/profile $prof $d2.token) 200
Expect "client cannot save a developer profile" (Call PUT /api/me/profile $prof $client.token) 403

Expect "proposal on a DRAFT project is refused (404)" (Call POST "/api/projects/$projId/proposals" @{ price = 900; message = $msg } $d1.token) 404
Call POST "/api/projects/$projId/publish" $null $client.token | Out-Null
Expect "developer without profile cannot propose (400)" (Call POST "/api/projects/$projId/proposals" @{ price = 900; message = $msg } $d3.token) 400

$p1 = Call POST "/api/projects/$projId/proposals" @{ price = 900; message = $msg } $d1.token
Expect "dev1 sends proposal" $p1 200
Expect "dev1 duplicate proposal is refused" (Call POST "/api/projects/$projId/proposals" @{ price = 800; message = $msg } $d1.token) 409
Expect "dev2 sends proposal" (Call POST "/api/projects/$projId/proposals" @{ price = 1100; message = $msg } $d2.token) 200
Expect "client cannot send a proposal" (Call POST "/api/projects/$projId/proposals" @{ price = 900; message = $msg } $client.token) 403

Expect "other client cannot list my proposals" (Call GET "/api/projects/$projId/proposals" $null $other.token) 404
$list = Call GET "/api/projects/$projId/proposals" $null $client.token
Expect "owner lists proposals" $list 200
"   proposals seen by owner: $($list.items.Count)   (want 2)"
Expect "other client cannot accept" (Call POST "/api/proposals/$($p1.id)/accept" $null $other.token) 404
Expect "owner accepts dev1" (Call POST "/api/proposals/$($p1.id)/accept" $null $client.token) 200
Expect "accepting again is refused" (Call POST "/api/proposals/$($p1.id)/accept" $null $client.token) 409

"   dev1 sees status: $((Call GET /api/me/proposals $null $d1.token).items[0].status)   (want accepted)"
"   dev2 sees status: $((Call GET /api/me/proposals $null $d2.token).items[0].status)   (want rejected)"
"   project status:   $((Call GET "/api/projects/$projId" $null $null).status)   (want in_progress)"
Expect "accepted proposal cannot be withdrawn" (Call POST "/api/proposals/$($p1.id)/withdraw" $null $d1.token) 404
