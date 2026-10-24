# Security policy

## Supported versions

Security fixes are released for the latest version of the provider only.

## Reporting a vulnerability

Do not open a public issue. Report the vulnerability privately through GitHub: on the repository's **Security** tab, select **Report a vulnerability**.

Include the provider version, the steps to reproduce and the impact you expect. You should get a first answer within 15 days. Once a fix is released, the advisory is published with credit to the reporter, unless you prefer to stay anonymous.

## Scope

In scope: the provider code, the PowerShell bridge scripts embedded in it, and the way it handles credentials (certificate passwords travel to PowerShell through the child process environment and are never written to disk or passed on the command line).

Out of scope: vulnerabilities in Microsoft Teams, the PowerShell modules or Terraform itself. Report those to their vendors. Credentials committed to your own Terraform configuration or state are also out of scope: mark them sensitive and protect your state backend.
