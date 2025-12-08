# Security Policy

## Supported Versions

We release patches for security vulnerabilities. Currently supported versions:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please bring it to our attention right away!

### Please do NOT:

- Open a public GitHub issue
- Discuss the vulnerability in public forums
- Exploit the vulnerability for malicious purposes

### Please DO:

1. **Report privately** via GitHub Security Advisories:
   - Navigate to the repository's Security tab
   - Click "Report a vulnerability"
   - Fill in the details

2. **Or email** the maintainers directly (if provided in repository settings)

3. **Include details**:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if available)

### What to expect:

- **Acknowledgment**: We'll acknowledge receipt within 48 hours
- **Updates**: We'll keep you informed of progress every 5-7 days
- **Fix timeline**: Critical issues will be addressed within 7 days, high severity within 30 days
- **Credit**: We'll credit you in the security advisory (unless you prefer to remain anonymous)

## Security Best Practices for Users

### Environment Variables

**Never commit secrets to version control:**
- ✅ Use `.env` file (gitignored)
- ✅ Use environment variables in production
- ❌ Don't hardcode API keys in code
- ❌ Don't commit `.env` files

### Production Deployment

**Before deploying to production:**

1. **Authentication**: Implement API key authentication (see SECURITY_REVIEW.md)
2. **Rate Limiting**: Add rate limiting to prevent abuse
3. **Request Limits**: Configure maximum request body size
4. **HTTPS**: Always use HTTPS in production
5. **Firewall**: Restrict access to trusted networks if possible
6. **Monitoring**: Set up logging and monitoring
7. **Updates**: Keep dependencies up to date

### API Security

**When using the API:**

- Use strong, unique API keys
- Rotate keys regularly
- Use HTTPS for all API calls
- Validate all inputs client-side
- Don't expose job IDs publicly
- Implement proper access controls

### Docker Security

**When running in Docker:**

```bash
# Run as non-root user
docker run --user 1000:1000 ...

# Limit resources
docker run --memory=2g --cpus=2 ...

# Use read-only filesystem where possible
docker run --read-only --tmpfs /tmp ...

# Don't expose unnecessary ports
docker run -p 127.0.0.1:8080:8080 ...
```

## Known Security Considerations

### Current Limitations

1. **No Built-in Authentication**: The API currently has no authentication mechanism. This must be added before production use.

2. **No Rate Limiting**: Without rate limiting, the API is vulnerable to abuse. Implement rate limiting middleware before production deployment.

3. **Predictable Job IDs**: Job IDs contain timestamps and short random components. Consider using UUIDs for better security.

See [SECURITY_REVIEW.md](SECURITY_REVIEW.md) for a complete security assessment.

## Security Updates

We will announce security updates through:
- GitHub Security Advisories
- Release notes
- Git tags (e.g., `v1.0.1-security`)

Subscribe to repository notifications to stay informed.

## Compliance

### Data Privacy

If you're processing personal data (e.g., faces in images):
- Ensure you have proper consent
- Implement data retention policies
- Support data deletion requests
- Follow GDPR/CCPA requirements as applicable

### Audit Logging

For compliance purposes, consider implementing:
- Request logging with timestamps
- Job creation/access logs
- API key usage tracking
- Security event monitoring

## Dependencies

We use automated tools to monitor dependencies:
- GitHub Dependabot (recommended to enable)
- `govulncheck` for Go vulnerabilities
- Regular dependency updates

## Security Tools Used

- **gosec**: Static security analyzer for Go
- **golangci-lint**: Comprehensive linting including security checks
- **CodeQL**: Semantic code analysis (can be enabled in GitHub Actions)

## Responsible Disclosure

We follow coordinated vulnerability disclosure:
1. Private report received
2. Vulnerability confirmed and assessed
3. Fix developed and tested
4. Security advisory published
5. Fix released
6. Public disclosure after 90 days or when fix is widely adopted

## Contact

For security-related questions or concerns:
- Use GitHub Security Advisories (preferred)
- Or contact repository maintainers directly

Thank you for helping keep InfiniteTalk API and its users safe!
