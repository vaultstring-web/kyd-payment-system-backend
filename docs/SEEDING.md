# Database Seeding Strategy

This document outlines the comprehensive database seeding strategy for the `kyd-payment-system`, focusing on populating development and testing environments with realistic, representative data.

## 1. Overview

The seeding script (`cmd/seed/main.go`) is designed to create consistent datasets across multiple runs, ensuring referential integrity, and offering configurable data volumes. It's an idempotent process, meaning it can be re-run without duplicating or corrupting existing data.

## 2. CLI Usage

The seeding script is executed via a CLI command with the following flags:

```bash
go run cmd/seed/main.go --env=<environment> --size=<size> --dry-run=<true/false>
```

*   `--env`: Specifies the target environment for seeding.
    *   `development` (default): Seeds the `kyd_dev` database. Truncates all relevant tables before seeding.
    *   `testing`: Seeds the `kyd_test` database. Truncates all relevant tables before seeding.
    *   `staging`: Seeds the `kyd_staging` database. **Skips truncation**, assuming the database might be populated from an anonymized production snapshot.
*   `--size`: Determines the volume of data to be generated.
    *   `small` (default): Generates a minimal dataset (e.g., 5 users, 10 transactions).
    *   `medium`: Generates a moderate dataset (e.g., 50 users, 200 transactions).
    *   `large`: Generates a substantial dataset (e.g., 500 users, 2000 transactions).
*   `--dry-run`: If `true`, the script will simulate the seeding process without making any actual changes to the database.

**Examples:**

```bash
# Seed a small dataset for development
go run cmd/seed/main.go --env=development --size=small

# Seed a large dataset for testing
go run cmd/seed/main.go --env=testing --size=large

# Perform a dry run for a medium dataset
go run cmd/seed/main.go --size=medium --dry-run=true
```

## 3. Extending Seeders for Schema Evolution

When the database schema evolves (e.g., new tables, new columns, modified relationships), the seeding script needs to be updated to reflect these changes.

### 3.1. Adding New Entities (New Tables)

1.  **Create a new `seed<EntityName>` function:**
    *   Define a new function (e.g., `seedProducts`) that takes `*sqlx.DB` and any necessary foreign keys (e.g., `userID`) as arguments.
    *   Generate realistic data for the new entity. Use `uuid.New()` for primary keys.
    *   Ensure all required fields are populated.
    *   Add `log.Fatalf` for any database insertion errors.
    *   Return the ID of the created entity if it's needed for subsequent seeding steps.

    ```go
    func seedProduct(db *sqlx.DB, userID string, name string, price float64) string {
        productID := uuid.New().String()
        _, err := db.Exec(`
            INSERT INTO customer_schema.products (id, user_id, name, price, created_at)
            VALUES ($1, $2, $3, $4, NOW())
        `, productID, userID, name, price)
        if err != nil {
            log.Fatalf("Failed to seed product %s: %v", name, err)
        }
        return productID
    }
    ```

2.  **Integrate into `main` function:**
    *   Add a call to your new `seed<EntityName>` function within the `main` function, ensuring it's placed after any dependencies (e.g., `seedProduct` should be called after `seedUser`).
    *   Consider adding configurable volume controls for the new entity based on the `--size` flag.

    ```go
    // ... in main function
    fmt.Println("Creating Products...")
    for i := 0; i < numProducts; i++ {
        seedProduct(db, userIDs[i%len(userIDs)], fmt.Sprintf("Product %d", i), float64(i*10+50))
    }
    ```

3.  **Update Truncation Logic:**
    *   If the new table is part of the `customer_schema` or `admin_schema`, add it to the `TRUNCATE` statements in the `main` function to ensure a clean slate on each run.

    ```go
    db.Exec("TRUNCATE TABLE customer_schema.products CASCADE")
    ```

4.  **Update Validation Logic:**
    *   Modify `validateSeededData` to include row count checks and integrity checks for the new table.

### 3.2. Modifying Existing Entities (New Columns)

1.  **Update `seed<EntityName>` function:**
    *   If a new column is added to an existing table (e.g., `users`), update the corresponding `seed<EntityName>` function (e.g., `seedUser`) to include the new column in the `INSERT` statement.
    *   Ensure the new column is populated with appropriate default or generated data.

    ```go
    // Example: Adding a 'status' field to User
    user := &domain.User{
        // ... existing fields
        UserStatus: domain.UserStatusActive, // New field
        // ...
    }
    ```

2.  **Update `UPDATE` statements (for idempotency):**
    *   If the `seed<EntityName>` function has logic to update existing records (like `seedUser`), ensure the `UPDATE` query also includes the new column if it needs to be reset or updated on subsequent runs.

### 3.3. Handling Relationships

*   When creating entities with foreign key relationships, always ensure the referenced entity exists first. The current script follows this pattern (users -> wallets -> transactions).
*   Pass the IDs of parent entities to child seeding functions.

### 3.4. Ensuring Idempotency

The current seeding script achieves idempotency primarily through:
*   **Truncation**: All relevant tables are truncated at the beginning of each run (except for staging environments). This is the primary mechanism for idempotency.
*   **Conditional Updates**: For core entities like `User`, the `seedUser` function checks if a user already exists by email and updates them instead of creating duplicates.

When extending seeders:
*   **Rely on Truncation**: For most new data, simply adding it to the truncation list is sufficient.
*   **Implement `FindBy` and `Update`**: If you need to preserve certain seeded data across runs (e.g., specific admin users) and avoid truncation for that entity, implement `FindBy` and `Update` logic similar to `seedUser`.

### 3.5. Updating Validation

*   After adding new entities or modifying existing ones, update the `validateSeededData` function:
    *   Add `COUNT(*)` queries for new tables to verify row counts.
    *   Add `LEFT JOIN` checks to ensure referential integrity for new foreign key relationships.

## 4. Best Practices

*   **Keep it Deterministic**: Ensure that given the same flags, the seeder always produces the exact same dataset. Avoid truly random data where possible, or use a fixed seed for random number generators.
*   **Performance**: Optimize seeding functions for bulk inserts where possible. Monitor the `Total seeding time` output to ensure benchmarks are met.
*   **Readability**: Keep seeding functions concise and focused on a single entity or a small group of related entities.
*   **Error Handling**: Use `log.Fatalf` for critical errors during seeding to stop the process immediately if data integrity is compromised.
*   **Environment Awareness**: Always consider how the `--env` flag should affect your new seeding logic.
*   **Version Control**: Keep `cmd/seed/main.go` and `docs/SEEDING.md` under version control.

By following these guidelines, the seeding script can be effectively maintained and extended as the `kyd-payment-system` schema evolves.
