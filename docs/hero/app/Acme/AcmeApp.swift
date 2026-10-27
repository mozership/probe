import SwiftUI

@main
struct AcmeApp: App {
    var body: some Scene {
        WindowGroup { SignInView() }
    }
}

struct SignInView: View {
    @State private var email = ""
    @State private var password = ""
    @State private var greeting: String?
    @State private var problem: String?

    // The API the app talks to, given as the launch argument -api.
    private let api = UserDefaults.standard.string(forKey: "api") ?? "https://api.example.com"

    var body: some View {
        NavigationStack {
            if let greeting {
                Text(greeting)
                    .font(.title2)
                    .accessibilityIdentifier("greeting")
                    .navigationTitle("Acme")
            } else {
                Form {
                    TextField("Email", text: $email)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.emailAddress)
                        .accessibilityIdentifier("email")
                    SecureField("Password", text: $password)
                        .accessibilityIdentifier("password")
                    Button("Sign in") { Task { await signIn() } }
                        .accessibilityIdentifier("signin")
                    if let problem {
                        Text(problem).accessibilityIdentifier("problem")
                    }
                }
                .navigationTitle("Sign in")
            }
        }
    }

    private func signIn() async {
        var req = URLRequest(url: URL(string: api + "/login")!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONEncoder().encode(["email": email, "password": password])
        do {
            let (data, res) = try await URLSession.shared.data(for: req)
            let body = (try? JSONDecoder().decode([String: String].self, from: data)) ?? [:]
            if (res as? HTTPURLResponse)?.statusCode == 200 {
                greeting = "Welcome, \(body["email"] ?? email)"
            } else {
                problem = body["error"] ?? "Sign-in failed"
            }
        } catch {
            problem = error.localizedDescription
        }
    }
}
