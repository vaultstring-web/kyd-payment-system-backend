# Google Services Integration Guide

This document provides instructions for configuring Google OAuth, Gmail API, and other Google Cloud services for the KYD Payment System.

## 1. Google OAuth 2.0 Configuration

The system uses Google OAuth for secure user authentication.

### Prerequisites
- A Google Cloud Platform (GCP) project.
- OAuth consent screen configured in GCP Console.

### Setup Steps
1. **Create Credentials**: Go to "APIs & Services" > "Credentials" > "Create Credentials" > "OAuth client ID".
2. **Application Type**: Select "Web application".
3. **Authorized Redirect URIs**:
   - Production: `https://api.yourdomain.com/api/v1/auth/google/callback`
   - Local: `http://localhost:9000/api/v1/auth/google/callback`
4. **Environment Variables**:
   - `GOOGLE_CLIENT_ID`: The Client ID from GCP.
   - `GOOGLE_CLIENT_SECRET`: The Client Secret from GCP.
   - `GOOGLE_MOCK_MODE`: Set to `false` to enable real Google login.

## 2. Gmail API Integration

For sending system emails (verification, notifications), the Gmail API is recommended over standard SMTP for better security.

### Configuration
1. **Enable Gmail API** in your GCP project.
2. **Service Account**: Create a Service Account and download the JSON key.
3. **Delegation**: Enable domain-wide delegation for the service account (if using Google Workspace).
4. **Environment Variables**:
   - `GMAIL_API_ENABLED`: `true`
   - `GOOGLE_SERVICE_ACCOUNT_PATH`: Path to the downloaded JSON key file.
   - `SMTP_USERNAME`: The email address to send from.

## 3. Other Google Services

The system configuration includes placeholders for additional services:

### Google Maps API
Used for address validation and location-based risk analysis.
- **Variable**: `GOOGLE_API_KEY`
- **Setup**: Create an API Key in GCP Console and restrict it to your production IP addresses.

### Google Cloud Storage (Future)
For storing KYC documents securely.
- **Variable**: `GOOGLE_PROJECT_ID`
- **Configuration**: Uses the same Service Account as the Gmail API.

## 4. Verification

To verify your Google integration:
1. Set `GOOGLE_MOCK_MODE=false`.
2. Start the system and click the "Google" button on the login page.
3. You should be redirected to the Google Sign-In screen.
4. After login, check the `audit` logs in the database to ensure the `AuthProvider` is recorded as `google`.
