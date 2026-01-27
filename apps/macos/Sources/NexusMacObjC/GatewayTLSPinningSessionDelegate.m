#import "GatewayTLSPinningSessionDelegate.h"

@implementation GatewayTLSPinningSessionDelegate

- (void)URLSession:(NSURLSession *)session
        didReceiveChallenge:(NSURLAuthenticationChallenge *)challenge
          completionHandler:(void (^)(NSURLSessionAuthChallengeDisposition disposition,
                                      NSURLCredential * _Nullable credential))completionHandler {
    if (self.challengeHandler != nil) {
        self.challengeHandler(session, challenge, completionHandler);
        return;
    }
    completionHandler(NSURLSessionAuthChallengePerformDefaultHandling, nil);
}

@end
