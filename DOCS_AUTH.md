# Authentication and OAuth Integration Documentation

## Overview
This document describes the implementation of Google OAuth integration and the password reset flow using the Gmail API in the KYD payment system.

## Google OAuth Integration

### Flow
1. **Initiation**: The user clicks the "Sign in with Google" button on the frontend.
2. **Redirect**: The frontend calls the backend `/api/v1/auth/google/start` endpoint, which returns a Google OAuth URL.
3. **Google Auth**: The user is redirected to Google, authenticates, and is redirected back to the frontend `/google-callback` page with an authorization code.
4. **Token Exchange**: The frontend sends the code to the backend `/api/v1/auth/google/callback`.
5. **User Management**: The backend validates the code, retrieves user info from Google, and either logs in the existing user or creates a new one.
6. **Session**: The backend sets authentication cookies and returns a JWT token.

### Security Measures
- **State Parameter**: Used to prevent CSRF attacks during the OAuth redirect flow.
- **ID Token Validation**: Backend validates Google ID tokens using Google's public keys.
- **Secure Cookies**: JWT tokens are stored in HttpOnly, Secure, and SameSite=Lax cookies.
- **Random Passwords**: New users created via Google OAuth are assigned a cryptographically secure random password (though they typically log in via Google).

## Password Reset Flow

### Flow
1. **Request**: User enters their email on the `/reset-password` page.
2. **Reset Email**: Backend generates a signed JWT reset token (valid for 1 hour) and sends a link via Gmail API.
3. **Reset**: User clicks the link, enters a new password on the frontend.
4. **Update**: Backend validates the token and updates the user's password in the database.

### Gmail API Integration
- The system uses the official Google Go API client to send emails.
- Configurable via environment variables:
  - `GMAIL_API_ENABLED`: Set to `true` to use Gmail API instead of SMTP.
  - `GMAIL_CREDENTIALS_PATH`: Path to the service account JSON file.

## Test Scenarios

### Google OAuth
- **Successful Login**: Existing user logs in via Google.
- **Successful Signup**: New user is created and logged in via Google.
- **Invalid Code**: Attempting to use an invalid or expired Google authorization code.
- **User Disconnect**: Handling cases where Google API is unreachable.

### Password Reset
- **Successful Request**: Valid email receives a reset link.
- **Email Enumeration Prevention**: Backend returns the same response even if the email doesn't exist.
- **Token Validation**: Ensuring expired or tampered tokens are rejected.
- **Password Strength**: Validating that new passwords meet the system's security requirements.

## Configuration
Add the following to your `.env` file:
```env
# Google OAuth
GOOGLE_CLIENT_ID=your-client-id
GOOGLE_CLIENT_SECRET=your-client-secret
GOOGLE_REDIRECT_URI=http://localhost:3000/google-callback

# Gmail API
GMAIL_API_ENABLED=true
GMAIL_CREDENTIALS_PATH=./config/gmail-credentials.json
```
