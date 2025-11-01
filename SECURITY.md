# Security Policy

## Supported Versions

We take security seriously and provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| Latest release | :white_check_mark: |
| Previous release | :white_check_mark: |
| < Previous release | :x: |

## Reporting a Vulnerability

We appreciate responsible disclosure of security vulnerabilities. If you discover a security issue, please follow these steps:

### 1. Do Not Create Public Issues

**Please do not report security vulnerabilities through public GitHub issues.** Public disclosure before a fix is available can put users at risk.

### 2. Report Privately

Send an email to our security team at: **[security@altairalabs.ai](mailto:security@altairalabs.ai)**

Include the following information in your report:
- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact and attack scenarios
- Any suggested fixes or mitigations
- Your contact information for follow-up

### 3. Encryption (Optional)

For highly sensitive reports, you may encrypt your email using our PGP key:

```
-----BEGIN PGP PUBLIC KEY BLOCK-----
[PGP key will be provided upon request]
-----END PGP PUBLIC KEY BLOCK-----
```

### 4. Response Timeline

We are committed to responding to security reports promptly:

- **Initial Response**: Within 48 hours of receiving your report
- **Triage**: Within 5 business days we will provide an initial assessment
- **Updates**: Regular updates on our progress every 5-10 business days
- **Resolution**: Timeline depends on severity and complexity, typically within 30-90 days

## Security Measures

PromptKit implements several security measures to protect users:

### Code Security

- **Static Analysis**: Automated security scanning of all code changes
- **Dependency Scanning**: Regular checks for known vulnerabilities in dependencies
- **Code Review**: All changes require review before merging
- **Signed Releases**: All releases are signed and checksummed

### Runtime Security

- **Input Validation**: Strict validation of all user inputs
- **Secure Defaults**: Safe configuration defaults
- **Principle of Least Privilege**: Minimal required permissions
- **Audit Logging**: Security-relevant events are logged

### Infrastructure Security

- **Secure Development**: Development follows secure coding practices
- **CI/CD Security**: Build pipelines use secure practices and isolated environments
- **Access Controls**: Multi-factor authentication and role-based access controls
- **Regular Updates**: Dependencies and infrastructure are regularly updated

## Security Considerations for Users

When using PromptKit, consider the following security best practices:

### API Keys and Credentials

- **Never commit API keys** to version control
- Use environment variables or secure credential stores
- Rotate keys regularly
- Use least-privilege API keys when possible

### Data Handling

- **Sensitive Data**: Be cautious when processing sensitive information with LLMs
- **Data Residency**: Understand where your data is processed and stored
- **Logging**: Be aware of what data might be logged during processing
- **Provider Security**: Review the security practices of your LLM providers

### Configuration Security

- **Validate Configurations**: Ensure configurations are from trusted sources
- **Network Security**: Use secure connections (HTTPS/TLS) for all communications
- **Access Controls**: Implement appropriate access controls for PromptKit deployments
- **Updates**: Keep PromptKit updated to the latest version

### Arena Testing Security

When using PromptKit Arena:

- **Test Data**: Use non-sensitive data in test scenarios
- **Isolation**: Run tests in isolated environments
- **Provider Limits**: Be aware of rate limits and costs when testing
- **Tool Execution**: Understand the security implications of tools used in tests

## Vulnerability Disclosure Policy

### Our Commitment

- We will work with security researchers to understand and fix reported vulnerabilities
- We will provide credit to researchers who report vulnerabilities responsibly
- We will not take legal action against researchers who follow this policy

### Researcher Guidelines

To be eligible for recognition:

- Follow responsible disclosure practices
- Do not access data that isn't your own
- Do not perform actions that could harm the service or other users
- Do not use social engineering against our employees or contractors
- Provide sufficient detail to reproduce the vulnerability

### Public Disclosure

Once a vulnerability is fixed:

1. We will publish a security advisory with details about the issue
2. We will credit the researcher(s) who reported the vulnerability (unless they prefer anonymity)
3. We may coordinate with the researcher on the timing of public disclosure

## Security Resources

- **Security Advisories**: [GitHub Security Advisories](https://github.com/AltairaLabs/PromptKit/security/advisories)
- **Security Contact**: [security@altairalabs.ai](mailto:security@altairalabs.ai)
- **General Contact**: [conduct@altairalabs.ai](mailto:conduct@altairalabs.ai)

## Compliance and Standards

PromptKit aims to follow industry security standards and best practices:

- **OWASP Guidelines**: Following OWASP secure coding practices
- **Supply Chain Security**: Using SLSA framework principles
- **OpenSSF**: Following Open Source Security Foundation guidelines
- **CVE Process**: Participating in CVE assignment for disclosed vulnerabilities

## Security Updates

Security updates are distributed through:

- **GitHub Releases**: Tagged releases with security fixes
- **Security Advisories**: GitHub security advisories for critical issues
- **Documentation**: Updated security documentation and guidelines
- **Community Channels**: Announcements in community forums and discussions

---

**Last Updated**: November 1, 2025  
**Next Review**: February 1, 2026

For questions about this security policy, contact: [security@altairalabs.ai](mailto:security@altairalabs.ai)