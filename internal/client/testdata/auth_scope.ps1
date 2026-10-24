# auth_scope.ps1 - lists the variables auth.ps1 assigns in the scope of the
# script that dot-sources it (run by auth_scope_test.go). Any variable assigned
# there would overwrite a script parameter of the same name, without regard to
# case.
param([string]$Script)
$ast = [System.Management.Automation.Language.Parser]::ParseFile($Script, [ref]$null, [ref]$null)
$assignments = $ast.FindAll({ $args[0] -is [System.Management.Automation.Language.AssignmentStatementAst] }, $true)
foreach ($a in $assignments) {
    $block = $a.Parent
    while ($block -isnot [System.Management.Automation.Language.ScriptBlockAst]) { $block = $block.Parent }
    if ($block -ne $ast) { continue }   # inside a script block: child scope
    $target = $a.Left
    while ($target -is [System.Management.Automation.Language.ConvertExpressionAst] -or
           $target -is [System.Management.Automation.Language.IndexExpressionAst]) {
        $target = if ($target -is [System.Management.Automation.Language.IndexExpressionAst]) { $target.Target } else { $target.Child }
    }
    "$($target.VariablePath.UserPath)"
}
